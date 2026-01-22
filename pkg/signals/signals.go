package signals

import (
	"time"

	"go.opentelemetry.io/collector/pdata/plog"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.opentelemetry.io/collector/pdata/ptrace"
)

type SignalType int

const (
	TypeTrace SignalType = iota
	TypeMetric
	TypeLog
)

// Context contains metadata about the received signal
type Context struct {
	ReceivedAt time.Time
	SourceAddr string
	// Add other metadata here (e.g., TenantID)
}

// Signal is a generic interface for all signal types
type Signal interface {
	Type() SignalType
	Context() Context
}

// TraceSignal wraps ptrace.Traces
type TraceSignal struct {
	Traces ptrace.Traces
	Ctx    Context
}

func (s *TraceSignal) Type() SignalType { return TypeTrace }
func (s *TraceSignal) Context() Context { return s.Ctx }

// MetricSignal wraps pmetric.Metrics
type MetricSignal struct {
	Metrics pmetric.Metrics
	Ctx     Context
}

func (s *MetricSignal) Type() SignalType { return TypeMetric }
func (s *MetricSignal) Context() Context { return s.Ctx }

// LogSignal wraps plog.Logs
type LogSignal struct {
	Logs plog.Logs
	Ctx  Context
}

func (s *LogSignal) Type() SignalType { return TypeLog }
func (s *LogSignal) Context() Context { return s.Ctx }
