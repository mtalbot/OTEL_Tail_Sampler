package decision

import (
	"testing"
	"time"

	"OTEL_Tail_Sampler/internal/config"
	"OTEL_Tail_Sampler/internal/gossip"
	"OTEL_Tail_Sampler/pkg/signals"

	"github.com/stretchr/testify/assert"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/ptrace"
)

type MockBroadcaster struct {
	Decisions []gossip.SamplingDecision
}

func (m *MockBroadcaster) BroadcastDecision(d gossip.SamplingDecision) error {
	m.Decisions = append(m.Decisions, d)
	return nil
}

func createTrace(latencyMs int64, errCode bool, attrs map[string]string) *signals.TraceSignal {
	traces := ptrace.NewTraces()
	rs := traces.ResourceSpans().AppendEmpty()
	ils := rs.ScopeSpans().AppendEmpty()
	span := ils.Spans().AppendEmpty()
	
	traceID := pcommon.TraceID([16]byte{1, 2, 3, 4, 1, 2, 3, 4, 1, 2, 3, 4, 1, 2, 3, 4})
	span.SetTraceID(traceID)
	
	start := time.Now()
	span.SetStartTimestamp(pcommon.NewTimestampFromTime(start))
	span.SetEndTimestamp(pcommon.NewTimestampFromTime(start.Add(time.Duration(latencyMs) * time.Millisecond)))
	
	if errCode {
		span.Status().SetCode(ptrace.StatusCodeError)
	}

	for k, v := range attrs {
		span.Attributes().PutStr(k, v)
	}

	return &signals.TraceSignal{
		Traces: traces,
		Ctx:    signals.Context{ReceivedAt: time.Now()},
	}
}

func TestEngine_LatencyTrigger(t *testing.T) {
	cfg := config.DecisionConfig{
		Policies: []config.PolicyConfig{
			{Name: "slow_trace", Type: "latency", LatencyThresholdMS: 100},
		},
	}
	mock := &MockBroadcaster{}
	engine := New(cfg, mock, true)

	// Test case 1: Fast trace (should not sample)
	engine.EvaluateTrace(createTrace(50, false, nil))
	assert.Len(t, mock.Decisions, 0)

	// Test case 2: Slow trace (should sample)
	engine.EvaluateTrace(createTrace(200, false, nil))
	assert.Len(t, mock.Decisions, 1)
	assert.Equal(t, "slow_trace", mock.Decisions[0].Reason)
}

func TestEngine_ErrorTrigger(t *testing.T) {
	cfg := config.DecisionConfig{
		Policies: []config.PolicyConfig{
			{Name: "err_trace", Type: "error"},
		},
	}
	mock := &MockBroadcaster{}
	engine := New(cfg, mock, true)

	engine.EvaluateTrace(createTrace(10, false, nil))
	assert.Len(t, mock.Decisions, 0)

	engine.EvaluateTrace(createTrace(10, true, nil))
	assert.Len(t, mock.Decisions, 1)
	assert.Equal(t, "err_trace", mock.Decisions[0].Reason)
}

func TestEngine_AttributeTrigger(t *testing.T) {
	cfg := config.DecisionConfig{
		Policies: []config.PolicyConfig{
			{Name: "vip_user", Type: "attribute", AttributeKey: "user.tier", AttributeValue: "gold"},
		},
	}
	mock := &MockBroadcaster{}
	engine := New(cfg, mock, true)

	engine.EvaluateTrace(createTrace(10, false, map[string]string{"user.tier": "silver"}))
	assert.Len(t, mock.Decisions, 0)

	engine.EvaluateTrace(createTrace(10, false, map[string]string{"user.tier": "gold"}))
	assert.Len(t, mock.Decisions, 1)
	assert.Equal(t, "vip_user", mock.Decisions[0].Reason)
}
