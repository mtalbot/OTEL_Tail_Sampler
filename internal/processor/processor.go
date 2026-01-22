package processor

import (
	"context"
	"encoding/hex"
	"log"
	"sync"
	"time"

	"OTEL_Tail_Sampler/internal/buffer"
	"OTEL_Tail_Sampler/internal/gossip"
	"OTEL_Tail_Sampler/internal/rollup"
	"OTEL_Tail_Sampler/pkg/signals"

	"go.opentelemetry.io/collector/pdata/plog"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.opentelemetry.io/collector/pdata/ptrace"
	"go.opentelemetry.io/collector/pdata/pcommon"
)

type Exporter interface {
	ExportTrace(trace *signals.TraceSignal) error
	ExportMetrics(metrics pmetric.Metrics) error
	ExportLogs(logs plog.Logs) error
}

type Processor struct {
	buffer   buffer.Manager
	gossip   *gossip.Manager
	rollup   *rollup.Processor
	exporter Exporter
	debug    bool

	// Temporary storage for signals evicted from buffer waiting for rollup
	evictedMetrics []*signals.MetricSignal
	evictedLogs    []*signals.LogSignal
	evictedMu      sync.Mutex
}

func New(buf buffer.Manager, gsp *gossip.Manager, rlp *rollup.Processor, exp Exporter, debug bool) *Processor {
	return &Processor{
		buffer:   buf,
		gossip:   gsp,
		rollup:   rlp,
		exporter: exp,
		debug:    debug,
		evictedMetrics: make([]*signals.MetricSignal, 0),
		evictedLogs:    make([]*signals.LogSignal, 0),
	}
}

func (p *Processor) log(format string, v ...interface{}) {
	if p.debug {
		log.Printf("[PROCESSOR] "+format, v...)
	}
}

func (p *Processor) Start(ctx context.Context) {
	p.log("Starting processor loops")
	go p.decisionLoop(ctx)
	go p.rollupLoop(ctx)
	go p.triggerLoop(ctx)
	go p.evictionConsumerLoop(ctx)
}

func (p *Processor) evictionConsumerLoop(ctx context.Context) {
	ch := p.buffer.GetEvictedSignals()
	for {
		select {
		case <-ctx.Done():
			return
		case sig := <-ch:
			p.handleEvictedSignal(sig)
		}
	}
}

func (p *Processor) handleEvictedSignal(sig signals.Signal) {
	p.evictedMu.Lock()
	defer p.evictedMu.Unlock()

	switch s := sig.(type) {
	case *signals.MetricSignal:
		p.evictedMetrics = append(p.evictedMetrics, s)
	case *signals.LogSignal:
		p.evictedLogs = append(p.evictedLogs, s)
	}
}

func (p *Processor) decisionLoop(ctx context.Context) {
	ch := p.gossip.GetDecisionChannel()
	for {
		select {
		case <-ctx.Done():
			return
		case d := <-ch:
			p.log("Received sampling decision for trace %s", d.TraceID)
			p.processDecision(d)
		}
	}
}

func (p *Processor) triggerLoop(ctx context.Context) {
	ch := p.gossip.GetTriggerChannel()
	for {
		select {
		case <-ctx.Done():
			return
		case t := <-ch:
			p.processTrigger(t)
		}
	}
}

func (p *Processor) processTrigger(t gossip.TriggerEvent) {
	p.log("Processing trigger %s (Value: %f)", t.Name, t.Value)
	
	if t.Name == "high_load" {
		if t.Value > t.Threshold {
			p.log("High load detected, switching to COARSE rollups")
			p.rollup.SetLevel(rollup.LevelCoarse)
		} else {
			p.log("Load normalized, switching back to FINE rollups")
			p.rollup.SetLevel(rollup.LevelFine)
		}
	}
}

func (p *Processor) processDecision(d gossip.SamplingDecision) {
	// Convert Hex string to TraceID
	b, err := hex.DecodeString(d.TraceID)
	if err != nil || len(b) != 16 {
		log.Printf("Invalid TraceID in decision: %s", d.TraceID)
		return
	}
	var tid pcommon.TraceID
	copy(tid[:], b)

	// 1. Fetch Trace
	trace, found := p.buffer.GetTrace(tid)
	if found {
		p.log("Found trace %s in buffer, exporting", d.TraceID)
		log.Printf("Sampling trace %s (Reason: %s)", d.TraceID, d.Reason)
		p.addProcessingLabelToTraces(trace.Traces)
		if err := p.exporter.ExportTrace(trace); err != nil {
			log.Printf("Failed to export trace %s: %v", d.TraceID, err)
		}
		p.buffer.RemoveTrace(tid)
	} else {
		p.log("Trace %s not found in local buffer", d.TraceID)
	}

	// 2. Fetch associated Logs
	traceLogs, foundLogs := p.buffer.GetTraceLogs(tid)
	if foundLogs {
		p.log("Found associated logs for trace %s, exporting", d.TraceID)
		p.addProcessingLabelToLogs(traceLogs.Logs)
		if err := p.exporter.ExportLogs(traceLogs.Logs); err != nil {
			log.Printf("Failed to export trace-bound logs for %s: %v", d.TraceID, err)
		}
	}

	// 3. Fetch Fine-Grained Metrics/Logs for the event window
	// Fetch slightly before and after (e.g. +/- 5s)
	start := d.EventStart.Add(-5 * time.Second)
	end := d.EventEnd.Add(5 * time.Second)
	p.log("Fetching fine-grained data for window %v to %v", start, end)

	metrics := p.buffer.GetMetricsInRange(start, end)
	if len(metrics) > 0 {
		p.log("Found %d fine-grained metrics, exporting", len(metrics))
		// Merge into one pmetric.Metrics and export WITHOUT rollup
		out := pmetric.NewMetrics()
		for _, m := range metrics {
			m.Metrics.ResourceMetrics().MoveAndAppendTo(out.ResourceMetrics())
		}
		p.addProcessingLabelToMetrics(out)
		p.exporter.ExportMetrics(out)
		p.buffer.RemoveMetricsInRange(start, end)
	}

	logs := p.buffer.GetLogsInRange(start, end)
	if len(logs) > 0 {
		p.log("Found %d fine-grained logs, exporting", len(logs))
		out := plog.NewLogs()
		for _, l := range logs {
			l.Logs.ResourceLogs().MoveAndAppendTo(out.ResourceLogs())
		}
		p.addProcessingLabelToLogs(out)
		p.exporter.ExportLogs(out)
		p.buffer.RemoveLogsInRange(start, end)
	}
}

func (p *Processor) rollupLoop(ctx context.Context) {
	// Periodic rollup of "everything else"
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.log("Starting periodic rollup")
			p.performRollup()
		}
	}
}

func (p *Processor) performRollup() {
	// 1. Fetch remaining unsampled data from buffer
	end := time.Now().Add(-10 * time.Second)
	start := time.Time{}

	bufferMetrics := p.buffer.GetMetricsInRange(start, end)
	bufferLogs := p.buffer.GetLogsInRange(start, end)

	// 2. Combine with auto-evicted data captured since last rollup
	p.evictedMu.Lock()
	allMetrics := append(bufferMetrics, p.evictedMetrics...)
	p.evictedMetrics = make([]*signals.MetricSignal, 0)
	
	allLogs := append(bufferLogs, p.evictedLogs...)
	p.evictedLogs = make([]*signals.LogSignal, 0)
	p.evictedMu.Unlock()

	// 3. Process Metrics
	if len(allMetrics) > 0 {
		p.log("Rolling up %d metrics (Buffer: %d, Evicted: %d)", len(allMetrics), len(bufferMetrics), len(allMetrics)-len(bufferMetrics))
		rolled := p.rollup.RollupMetrics(allMetrics)
		p.addProcessingLabelToMetrics(rolled)
		p.exporter.ExportMetrics(rolled)
		p.buffer.RemoveMetricsInRange(start, end)
	}

	// 4. Process Logs
	if len(allLogs) > 0 {
		p.log("Rolling up %d logs (Buffer: %d, Evicted: %d)", len(allLogs), len(bufferLogs), len(allLogs)-len(bufferLogs))
		rolled := p.rollup.RollupLogs(allLogs)
		p.addProcessingLabelToLogs(rolled)
		p.exporter.ExportLogs(rolled)
		p.buffer.RemoveLogsInRange(start, end)
	}
}

func (p *Processor) addProcessingLabelToTraces(t ptrace.Traces) {
	rms := t.ResourceSpans()
	for i := 0; i < rms.Len(); i++ {
		rms.At(i).Resource().Attributes().PutBool("sampler.processed", true)
	}
}

func (p *Processor) addProcessingLabelToMetrics(m pmetric.Metrics) {
	rms := m.ResourceMetrics()
	for i := 0; i < rms.Len(); i++ {
		rms.At(i).Resource().Attributes().PutBool("sampler.processed", true)
	}
}

func (p *Processor) addProcessingLabelToLogs(l plog.Logs) {
	rls := l.ResourceLogs()
	for i := 0; i < rls.Len(); i++ {
		rls.At(i).Resource().Attributes().PutBool("sampler.processed", true)
	}
}
