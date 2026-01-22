package buffer

import (
	"context"
	"testing"
	"time"

	"OTEL_Tail_Sampler/internal/config"
	"OTEL_Tail_Sampler/pkg/signals"

	"github.com/stretchr/testify/assert"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/plog"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.opentelemetry.io/collector/pdata/ptrace"
)

func createTraceSignal(traceID pcommon.TraceID) *signals.TraceSignal {
	traces := ptrace.NewTraces()
	rs := traces.ResourceSpans().AppendEmpty()
	ils := rs.ScopeSpans().AppendEmpty()
	span := ils.Spans().AppendEmpty()
	span.SetTraceID(traceID)
	span.SetSpanID(pcommon.SpanID([8]byte{1, 2, 3, 4, 5, 6, 7, 8}))
	span.SetName("test-span")
	
	return &signals.TraceSignal{
		Traces: traces,
		Ctx: signals.Context{
			ReceivedAt: time.Now(),
		},
	}
}

func getAllMetrics(buf *InMemoryBuffer) []*signals.MetricSignal {
	return buf.GetMetricsInRange(time.Time{}, time.Now().Add(time.Hour))
}

func getAllLogs(buf *InMemoryBuffer) []*signals.LogSignal {
	return buf.GetLogsInRange(time.Time{}, time.Now().Add(time.Hour))
}

func TestInMemoryBuffer_StoreAndGet(t *testing.T) {
	cfg := config.BufferConfig{
		MaxTraces:  10,
		TTLSeconds: 60,
	}
	buf := New(cfg, true)
	// BigCache init takes a moment sometimes? No, sync.
	
	traceID := pcommon.TraceID([16]byte{1, 2, 3, 4, 1, 2, 3, 4, 1, 2, 3, 4, 1, 2, 3, 4})
	sig := createTraceSignal(traceID)

	err := buf.Store(sig)
	assert.NoError(t, err)

	// Retrieve
	out, ok := buf.GetTrace(traceID)
	assert.True(t, ok)
	assert.Equal(t, 1, out.Traces.SpanCount())
	
	// Check Span
	span := out.Traces.ResourceSpans().At(0).ScopeSpans().At(0).Spans().At(0)
	assert.Equal(t, traceID, span.TraceID())
}

// NOTE: TestInMemoryBuffer_Eviction is removed or needs update because
// BigCache handles eviction asynchronously and not strictly on "MaxTraces" count but on size/TTL.
// The previous implementation had explicit LRU on count. 
// BigCache is "HardMaxCacheSize" (MB) based.
// We can't easily test exact item eviction based on count anymore.
// We will skip testing exact eviction count.

func TestInMemoryBuffer_TTL(t *testing.T) {
	cfg := config.BufferConfig{
		MaxTraces:  10,
		TTLSeconds: 1,
	}
	buf := New(cfg, true)
	buf.Start(context.Background())
	defer buf.Stop()

	id1 := pcommon.TraceID([16]byte{1})
	buf.Store(createTraceSignal(id1))

	// Add Metric and Log
	m := &signals.MetricSignal{
		Metrics: pmetric.NewMetrics(),
		Ctx:     signals.Context{ReceivedAt: time.Now()},
	}
	
	logs := plog.NewLogs()
	logs.ResourceLogs().AppendEmpty().ScopeLogs().AppendEmpty().LogRecords().AppendEmpty().Body().SetStr("test")
	l := &signals.LogSignal{
		Logs: logs,
		Ctx:  signals.Context{ReceivedAt: time.Now()},
	}
	buf.Store(m)
	buf.Store(l)

	// Check exist
	_, ok := buf.GetTrace(id1)
	assert.True(t, ok)
	assert.Len(t, getAllMetrics(buf), 1)
	assert.Len(t, getAllLogs(buf), 1)

	// Wait for TTL (1s) + cleanup loop (5s) + buffer
	// BigCache has its own TTL. Our cleanup loop for indices runs every 5s.
	// So we need to wait > 5s? 
	// Or we can verify that BigCache returns error (Not Found) even if index exists?
	// Our GetMetricsInRange checks cache.Get() return value.
	
	// BigCache TTL resolution is 1s.
	time.Sleep(2500 * time.Millisecond)

	// Should be gone
	_, ok = buf.GetTrace(id1)
	assert.False(t, ok, "ID1 should be expired")
	
	// Metrics/Logs should be gone from retrieval even if index lingers
	assert.Len(t, getAllMetrics(buf), 0)
	assert.Len(t, getAllLogs(buf), 0)
}

func TestInMemoryBuffer_MetricsLogs(t *testing.T) {
	cfg := config.BufferConfig{
		TTLSeconds: 60,
	}
	buf := New(cfg, true)

	buf.Store(&signals.MetricSignal{Metrics: pmetric.NewMetrics(), Ctx: signals.Context{ReceivedAt: time.Now()}})
	buf.Store(&signals.MetricSignal{Metrics: pmetric.NewMetrics(), Ctx: signals.Context{ReceivedAt: time.Now()}})
	
	logs := plog.NewLogs()
	logs.ResourceLogs().AppendEmpty().ScopeLogs().AppendEmpty().LogRecords().AppendEmpty().Body().SetStr("test")
	buf.Store(&signals.LogSignal{Logs: logs, Ctx: signals.Context{ReceivedAt: time.Now()}})

	assert.Len(t, getAllMetrics(buf), 2)
	assert.Len(t, getAllLogs(buf), 1)

	buf.ClearMetrics()
	assert.Len(t, getAllMetrics(buf), 0)
	assert.Len(t, getAllLogs(buf), 1)
}