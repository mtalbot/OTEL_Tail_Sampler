package decision

import (
	"log"
	"time"

	"OTEL_Tail_Sampler/internal/config"
	"OTEL_Tail_Sampler/internal/gossip"
	"OTEL_Tail_Sampler/pkg/signals"

	"go.opentelemetry.io/collector/pdata/ptrace"
)

type Broadcaster interface {
	BroadcastDecision(decision gossip.SamplingDecision) error
}

type Engine struct {
	cfg         config.DecisionConfig
	broadcaster Broadcaster
	debug       bool
}

func New(cfg config.DecisionConfig, broadcaster Broadcaster, debug bool) *Engine {
	return &Engine{
		cfg:         cfg,
		broadcaster: broadcaster,
		debug:       debug,
	}
}

func (e *Engine) log(format string, v ...interface{}) {
	if e.debug {
		log.Printf("[DECISION] "+format, v...)
	}
}

// EvaluateTrace checks a trace against policies
func (e *Engine) EvaluateTrace(trace *signals.TraceSignal) {
	e.log("Evaluating trace signal")
	// Iterate over policies
	for _, policy := range e.cfg.Policies {
		matched := false
		switch policy.Type {
		case "error":
			matched = e.checkError(trace)
		case "latency":
			matched = e.checkLatency(trace, policy.LatencyThresholdMS)
		case "attribute":
			matched = e.checkAttribute(trace, policy.AttributeKey, policy.AttributeValue)
		// case "probabilistic": ...
		}

		if matched {
			e.log("Matched policy %s", policy.Name)
			e.triggerDecision(trace, policy.Name)
			return // Decided
		}
	}
}

func (e *Engine) checkError(trace *signals.TraceSignal) bool {
	rss := trace.Traces.ResourceSpans()
	for i := 0; i < rss.Len(); i++ {
		rs := rss.At(i)
		ilss := rs.ScopeSpans()
		for j := 0; j < ilss.Len(); j++ {
			ils := ilss.At(j)
			spans := ils.Spans()
			for k := 0; k < spans.Len(); k++ {
				span := spans.At(k)
				if span.Status().Code() == ptrace.StatusCodeError {
					return true
				}
			}
		}
	}
	return false
}

func (e *Engine) checkLatency(trace *signals.TraceSignal, threshold int64) bool {
	thresholdNs := int64(threshold) * 1_000_000
	rss := trace.Traces.ResourceSpans()
	for i := 0; i < rss.Len(); i++ {
		rs := rss.At(i)
		ilss := rs.ScopeSpans()
		for j := 0; j < ilss.Len(); j++ {
			ils := ilss.At(j)
			spans := ils.Spans()
			for k := 0; k < spans.Len(); k++ {
				span := spans.At(k)
				duration := span.EndTimestamp().AsTime().Sub(span.StartTimestamp().AsTime()).Nanoseconds()
				if duration > thresholdNs {
					return true
				}
			}
		}
	}
	return false
}

func (e *Engine) checkAttribute(trace *signals.TraceSignal, key, value string) bool {
	rss := trace.Traces.ResourceSpans()
	for i := 0; i < rss.Len(); i++ {
		rs := rss.At(i)
		ilss := rs.ScopeSpans()
		for j := 0; j < ilss.Len(); j++ {
			ils := ilss.At(j)
			spans := ils.Spans()
			for k := 0; k < spans.Len(); k++ {
				span := spans.At(k)
				if v, ok := span.Attributes().Get(key); ok {
					if v.Str() == value {
						return true
					}
				}
			}
		}
	}
	return false
}

func (e *Engine) triggerDecision(trace *signals.TraceSignal, reason string) {
	var traceID string
	var start, end time.Time
	
	// Scan all spans to find the overall time window and first TraceID
	rss := trace.Traces.ResourceSpans()
	for i := 0; i < rss.Len(); i++ {
		sls := rss.At(i).ScopeSpans()
		for j := 0; j < sls.Len(); j++ {
			spans := sls.At(j).Spans()
			for k := 0; k < spans.Len(); k++ {
				span := spans.At(k)
				if traceID == "" && !span.TraceID().IsEmpty() {
					traceID = span.TraceID().String()
				}
				
				s := span.StartTimestamp().AsTime()
				if start.IsZero() || s.Before(start) {
					start = s
				}
				e := span.EndTimestamp().AsTime()
				if end.IsZero() || e.After(end) {
					end = e
				}
			}
		}
	}

	if traceID == "" {
		return
	}

	decision := gossip.SamplingDecision{
		TraceID:    traceID,
		Reason:     reason,
		DecidedAt:  time.Now(),
		DecidedBy:  "local", // TODO: use hostname
		EventStart: start,
		EventEnd:   end,
	}

	log.Printf("Triggering sampling for trace %s due to %s", traceID, reason)

	// Broadcast
	if e.broadcaster != nil {
		if err := e.broadcaster.BroadcastDecision(decision); err != nil {
			log.Printf("Failed to broadcast decision: %v", err)
		}
	}
}
