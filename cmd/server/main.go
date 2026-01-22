package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"OTEL_Tail_Sampler/internal/buffer"
	"OTEL_Tail_Sampler/internal/config"
	"OTEL_Tail_Sampler/internal/decision"
	"OTEL_Tail_Sampler/internal/discovery"
	"OTEL_Tail_Sampler/internal/exporter"
	"OTEL_Tail_Sampler/internal/gossip"
	"OTEL_Tail_Sampler/internal/processor"
	"OTEL_Tail_Sampler/internal/receiver"
	"OTEL_Tail_Sampler/internal/rollup"
	"OTEL_Tail_Sampler/internal/wal"
	"OTEL_Tail_Sampler/pkg/signals"

	"gopkg.in/yaml.v3"
)

// mainConsumer bridges the receiver and the rest of the system
type mainConsumer struct {
	buffer   buffer.Manager
	decision *decision.Engine
	wal      *wal.Log
	debug    bool
}

func (c *mainConsumer) Consume(sig signals.Signal) error {
	if c.debug {
		log.Printf("[CONSUMER] Received signal of type %v", sig.Type())
	}
	// 1. Write to WAL first for durability
	if c.wal != nil {
		if err := c.wal.Write(sig); err != nil {
			log.Printf("Failed to write to WAL: %v", err)
			return err
		}
	}

	// 2. Store in buffer
	if err := c.buffer.Store(sig); err != nil {
		log.Printf("Failed to store signal: %v", err)
		return err
	}

	// 3. If it's a trace, evaluate for immediate sampling decision
	if ts, ok := sig.(*signals.TraceSignal); ok {
		c.decision.EvaluateTrace(ts)
	}

	return nil
}

func main() {
	// 1. Load Configuration
	cfg := config.DefaultConfig()
	
	// Load from file if exists (for Helm/K8s)
	configPath := "/etc/otel-sampler/config.yaml"
	if _, err := os.Stat(configPath); err == nil {
		data, err := os.ReadFile(configPath)
		if err == nil {
			if err := yaml.Unmarshal(data, cfg); err != nil {
				log.Printf("Warning: Failed to parse config file: %v", err)
			} else {
				log.Printf("Loaded config from %s", configPath)
			}
		}
	}
	
	// Simple override for integration tests/docker
	if ep := os.Getenv("EXPORTER_ENDPOINT"); ep != "" {
		cfg.Exporter.Endpoint = ep
	}
	if host := os.Getenv("RECEIVER_HOST"); host != "" {
		cfg.Receiver.Host = host
	}

	log.Printf("Starting OTEL Tail Sampler with exporter: %s (Debug: %v)", cfg.Exporter.Endpoint, cfg.Debug)

	// Context for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 2. Initialize Components
	
	// WAL
	walLog, err := wal.Open("./data/wal")
	if err != nil {
		log.Fatalf("Failed to open WAL: %v", err)
	}
	defer walLog.Close()

	// Exporter
	exp, err := exporter.New(cfg.Exporter)
	if err != nil {
		log.Fatalf("Failed to initialize exporter: %v", err)
	}
	defer exp.Stop()

	// Buffer
	buf := buffer.New(cfg.Buffer, cfg.Debug)
	if err := buf.Start(ctx); err != nil {
		log.Fatalf("Failed to start buffer: %v", err)
	}
	defer buf.Stop()

	// Replay WAL to re-populate buffer after restart
	log.Println("Replaying WAL...")
	err = walLog.Replay(func(sig signals.Signal) {
		buf.Store(sig)
	})
	if err != nil {
		log.Printf("WAL replay error: %v", err)
	}

	// Discovery
	disc, err := discovery.New(cfg.Discovery)
	if err != nil {
		log.Fatalf("Failed to initialize discovery: %v", err)
	}

	// Gossip
	gsp := gossip.New(cfg.Gossip, disc, cfg.Debug)
	if err := gsp.Start(ctx); err != nil {
		log.Fatalf("Failed to start gossip: %v", err)
	}
	defer gsp.Stop()

	// Decision Engine
	engine := decision.New(cfg.Decision, gsp, cfg.Debug)

	// Rollup Processor
	rlp := rollup.New(cfg.Rollup)

	// Main Processor (Handles decisions and periodic rollups)
	proc := processor.New(buf, gsp, rlp, exp, cfg.Debug)
	proc.Start(ctx)

	// Load Monitoring Loop
	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		
		// For prototype: monitor buffer saturation
		threshold := float64(cfg.Buffer.MaxTraces) * 0.8
		highLoadActive := false

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				count := buf.GetTraceCount()

				if float64(count) > threshold && !highLoadActive {
					gsp.BroadcastTrigger(gossip.TriggerEvent{
						Name:      "high_load",
						Value:     float64(count),
						Threshold: threshold,
						StartedAt: time.Now(),
					})
					highLoadActive = true
				} else if float64(count) <= threshold && highLoadActive {
					gsp.BroadcastTrigger(gossip.TriggerEvent{
						Name:      "high_load",
						Value:     float64(count),
						Threshold: threshold,
						StartedAt: time.Now(),
					})
					highLoadActive = false
				}
			}
		}
	}()

	// WAL Truncation Loop
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				// Keep last 10 minutes of WAL
				cutoff := time.Now().Add(-10 * time.Minute)
				if err := walLog.TruncateBefore(cutoff); err != nil {
					log.Printf("WAL truncation error: %v", err)
				}
			}
		}
	}()

	// Receiver
	consumer := &mainConsumer{
		buffer:   buf,
		decision: engine,
		wal:      walLog,
		debug:    cfg.Debug,
	}
	rcv := receiver.New(cfg.Receiver, consumer, cfg.Debug)
	if err := rcv.Start(ctx); err != nil {
		log.Fatalf("Failed to start receiver: %v", err)
	}
	defer rcv.Stop()

	log.Println("Service initialized and running. Press Ctrl+C to stop.")

	// 3. Wait for Signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	log.Println("Shutting down...")
	log.Println("Shutdown complete.")
}