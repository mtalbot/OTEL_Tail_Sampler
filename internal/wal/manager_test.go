package wal

import (
	"os"
	"testing"
	"time"

	"OTEL_Tail_Sampler/pkg/signals"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/pdata/ptrace"
)

func TestWAL_WriteReplay(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "wal-test")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	l, err := Open(tmpDir)
	require.NoError(t, err)

	// Create a trace signal
	traces := ptrace.NewTraces()
	traces.ResourceSpans().AppendEmpty().ScopeSpans().AppendEmpty().Spans().AppendEmpty().SetName("test-span")
	sig := &signals.TraceSignal{
		Traces: traces,
		Ctx:    signals.Context{ReceivedAt: time.Now().Round(time.Second)},
	}

	// Write
	err = l.Write(sig)
	require.NoError(t, err)
	l.Close()

	// Reopen and Replay
	l2, err := Open(tmpDir)
	require.NoError(t, err)
	defer l2.Close()

	var replayed []signals.Signal
	err = l2.Replay(func(s signals.Signal) {
		replayed = append(replayed, s)
	})
	require.NoError(t, err)

	assert.Len(t, replayed, 1)
	rsig, ok := replayed[0].(*signals.TraceSignal)
	assert.True(t, ok)
	assert.Equal(t, "test-span", rsig.Traces.ResourceSpans().At(0).ScopeSpans().At(0).Spans().At(0).Name())
	assert.True(t, sig.Ctx.ReceivedAt.Equal(rsig.Ctx.ReceivedAt))
}

func TestWAL_Truncate(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "wal-truncate-test")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	l, err := Open(tmpDir)
	require.NoError(t, err)
	defer l.Close()

	t1 := time.Now().Add(-10 * time.Minute).Round(time.Second)
	t2 := time.Now().Round(time.Second)

	sig1 := &signals.TraceSignal{Traces: ptrace.NewTraces(), Ctx: signals.Context{ReceivedAt: t1}}
	sig2 := &signals.TraceSignal{Traces: ptrace.NewTraces(), Ctx: signals.Context{ReceivedAt: t2}}

	l.Write(sig1)
	l.Write(sig2)

	// Truncate before now - 5 min
	err = l.TruncateBefore(time.Now().Add(-5 * time.Minute))
	require.NoError(t, err)

	var replayed []signals.Signal
	l.Replay(func(s signals.Signal) {
		replayed = append(replayed, s)
	})

	assert.Len(t, replayed, 1)
	assert.True(t, t2.Equal(replayed[0].Context().ReceivedAt))
}
