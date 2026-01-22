package buffer

import (
	"testing"
	"time"

	"OTEL_Tail_Sampler/internal/config"
	"OTEL_Tail_Sampler/pkg/signals"

	"github.com/stretchr/testify/assert"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/plog"
)

func createLogSignalWithTraceID(traceID pcommon.TraceID) *signals.LogSignal {
	logs := plog.NewLogs()
	rl := logs.ResourceLogs().AppendEmpty()
	sl := rl.ScopeLogs().AppendEmpty()
	lr := sl.LogRecords().AppendEmpty()
	lr.SetTraceID(traceID)
	lr.Body().SetStr("test log with traceID")
	
	return &signals.LogSignal{
		Logs: logs,
		Ctx: signals.Context{
			ReceivedAt: time.Now(),
		},
	}
}

func createLogSignalNoTraceID() *signals.LogSignal {
	logs := plog.NewLogs()
	rl := logs.ResourceLogs().AppendEmpty()
	sl := rl.ScopeLogs().AppendEmpty()
	lr := sl.LogRecords().AppendEmpty()
	lr.Body().SetStr("test log NO traceID")
	
	return &signals.LogSignal{
		Logs: logs,
		Ctx: signals.Context{
			ReceivedAt: time.Now(),
		},
	}
}

func getLogs(buf *InMemoryBuffer) []*signals.LogSignal {
	return buf.GetLogsInRange(time.Time{}, time.Now().Add(time.Hour))
}

func TestInMemoryBuffer_TraceLogs(t *testing.T) {
	cfg := config.BufferConfig{
		MaxTraces:  10,
		TTLSeconds: 60,
	}
	buf := New(cfg, true)

	tid := pcommon.TraceID([16]byte{1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1})
	
	// Store Log WITH TraceID
	err := buf.Store(createLogSignalWithTraceID(tid))
	assert.NoError(t, err)

	// Verify it is NOT in the general logs buffer (GetLogs)
	assert.Len(t, getLogs(buf), 0, "Log with TraceID should not be in general buffer")

	// Retrieve Trace Logs
	lOut, ok := buf.GetTraceLogs(tid)
	assert.True(t, ok)
	assert.Equal(t, 1, lOut.Logs.LogRecordCount())
	
	// Retrieve Trace (should be missing as no spans yet)
	// BigCache impl: GetTrace checks TracesData. If empty, returns false.
	_, ok = buf.GetTrace(tid)
	assert.False(t, ok)

	// Store Log WITHOUT TraceID
	buf.Store(createLogSignalNoTraceID())
	assert.Len(t, getLogs(buf), 1, "Log without TraceID SHOULD be in general buffer")

	// Add Trace Span
	trSig := createTraceSignal(tid)
	buf.Store(trSig)

	// Now Trace should exist
	tOut, ok := buf.GetTrace(tid)
	assert.True(t, ok)
	assert.Equal(t, 1, tOut.Traces.SpanCount())
	
	// Logs should still exist attached to trace
	lOut, ok = buf.GetTraceLogs(tid)
	assert.True(t, ok)
	assert.Equal(t, 1, lOut.Logs.LogRecordCount())

	// Remove Trace
	buf.RemoveTrace(tid)

	// Verify both gone from TraceData map
	_, ok = buf.GetTrace(tid)
	assert.False(t, ok)
	_, ok = buf.GetTraceLogs(tid)
	assert.False(t, ok)
	
	// General logs should still be there
	assert.Len(t, getLogs(buf), 1)
}
