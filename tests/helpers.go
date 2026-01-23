package tests

import (
	"net"
	"sync"
	"testing"

	"OTEL_Tail_Sampler/internal/buffer"
	"OTEL_Tail_Sampler/internal/decision"
	"OTEL_Tail_Sampler/internal/wal"
	"OTEL_Tail_Sampler/pkg/signals"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/pdata/plog"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.opentelemetry.io/collector/pdata/ptrace"
)

type MockExporter struct {
	mu sync.Mutex
	Traces  []ptrace.Traces
	Metrics []pmetric.Metrics
	Logs    []plog.Logs
}

func (m *MockExporter) ExportTrace(sig *signals.TraceSignal) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Traces = append(m.Traces, sig.Traces)
	return nil
}

func (m *MockExporter) ExportMetrics(met pmetric.Metrics) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Metrics = append(m.Metrics, met)
	return nil
}

func (m *MockExporter) ExportLogs(l plog.Logs) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Logs = append(m.Logs, l)
	return nil
}

type consumerBridge struct {
	buffer   buffer.Manager
	decision *decision.Engine
	wal      *wal.Log
}

func (c *consumerBridge) Consume(sig signals.Signal) error {
	if c.wal != nil {
		if err := c.wal.Write(sig); err != nil {
			return err
		}
	}
	if err := c.buffer.Store(sig); err != nil {
		return err
	}
	if ts, ok := sig.(*signals.TraceSignal); ok {
		c.decision.EvaluateTrace(ts)
	}
	return nil
}

func getFreePort(t *testing.T) int {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	return port
}
