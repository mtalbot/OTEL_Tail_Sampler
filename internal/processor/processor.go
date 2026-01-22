package processor

import (
	"context"
	"encoding/hex"
	"log"
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
}

func New(buf buffer.Manager, gsp *gossip.Manager, rlp *rollup.Processor, exp Exporter, debug bool) *Processor {
	return &Processor{
		buffer:   buf,
		gossip:   gsp,
		rollup:   rlp,
		exporter: exp,
		debug:    debug,
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
	// Everything in buffer currently is "unsampled" (or hasn't been removed by a decision yet)
	// We use a very wide range to fetch everything up to "now - safe margin"
	end := time.Now().Add(-10 * time.Second)
	start := time.Time{}

	metrics := p.buffer.GetMetricsInRange(start, end)
	if len(metrics) > 0 {
		p.log("Rolling up %d metrics", len(metrics))
		rolled := p.rollup.RollupMetrics(metrics)
		p.addProcessingLabelToMetrics(rolled)
		p.exporter.ExportMetrics(rolled)
		p.buffer.RemoveMetricsInRange(start, end)
	}

	logs := p.buffer.GetLogsInRange(start, end)
	if len(logs) > 0 {
		p.log("Rolling up %d logs", len(logs))
		rolled := p.rollup.RollupLogs(logs)
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