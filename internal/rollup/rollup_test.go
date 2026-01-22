package rollup

import (
	"testing"

	"OTEL_Tail_Sampler/internal/config"
	"OTEL_Tail_Sampler/pkg/signals"

	"github.com/stretchr/testify/assert"
	"go.opentelemetry.io/collector/pdata/plog"
	"go.opentelemetry.io/collector/pdata/pmetric"
)

func createMetricSignal(val int64, method string, userID string) *signals.MetricSignal {
	metrics := pmetric.NewMetrics()
	rm := metrics.ResourceMetrics().AppendEmpty()
	sm := rm.ScopeMetrics().AppendEmpty()
	m := sm.Metrics().AppendEmpty()
	m.SetName("http_requests")
	sum := m.SetEmptySum()
	dp := sum.DataPoints().AppendEmpty()
	dp.SetIntValue(val)
	dp.Attributes().PutStr("http.method", method)
	dp.Attributes().PutStr("user_id", userID)
	
	return &signals.MetricSignal{
		Metrics: metrics,
	}
}

func TestRollupMetrics_LevelMedium(t *testing.T) {
	proc := New(config.RollupConfig{Enabled: true})
	proc.SetLevel(LevelMedium)

	// Two signals with same method but different user IDs
	s1 := createMetricSignal(10, "GET", "user-1")
	s2 := createMetricSignal(5, "GET", "user-2")
	s3 := createMetricSignal(7, "POST", "user-1")

	result := proc.RollupMetrics([]*signals.MetricSignal{s1, s2, s3})

	// Should have aggregated s1 and s2 because user_id is stripped in LevelMedium
	// But s3 remains separate because method is different
	
	count := 0
	rms := result.ResourceMetrics()
	for i := 0; i < rms.Len(); i++ {
		sms := rms.At(i).ScopeMetrics()
		for j := 0; j < sms.Len(); j++ {
			ms := sms.At(j).Metrics()
			for k := 0; k < ms.Len(); k++ {
				dps := ms.At(k).Sum().DataPoints()
				for l := 0; l < dps.Len(); l++ {
					dp := dps.At(l)
					count++
					
					method, _ := dp.Attributes().Get("http.method")
					if method.Str() == "GET" {
						assert.Equal(t, int64(15), dp.IntValue())
					} else {
						assert.Equal(t, int64(7), dp.IntValue())
					}
					
					// user_id should be GONE
					_, ok := dp.Attributes().Get("user_id")
					assert.False(t, ok)
				}
			}
		}
	}
	assert.Equal(t, 2, count)
}

func TestRollupMetrics_LevelCoarse(t *testing.T) {
	proc := New(config.RollupConfig{Enabled: true})
	proc.SetLevel(LevelCoarse)

	s1 := createMetricSignal(10, "GET", "user-1")
	s2 := createMetricSignal(5, "POST", "user-2")

	result := proc.RollupMetrics([]*signals.MetricSignal{s1, s2})

	// LevelCoarse strips EVERYTHING (even method)
	// So s1 and s2 should merge into ONE data point
	
	rms := result.ResourceMetrics()
	dpCount := 0
	total := int64(0)
	for i := 0; i < rms.Len(); i++ {
		sms := rms.At(i).ScopeMetrics()
		for j := 0; j < sms.Len(); j++ {
			ms := sms.At(j).Metrics()
			for k := 0; k < ms.Len(); k++ {
				dps := ms.At(k).Sum().DataPoints()
				dpCount += dps.Len()
				for l := 0; l < dps.Len(); l++ {
					total += dps.At(l).IntValue()
				}
			}
		}
	}
	assert.Equal(t, 1, dpCount)
	assert.Equal(t, int64(15), total)
}

func createLogSignal(body string, severity plog.SeverityNumber) *signals.LogSignal {
	logs := plog.NewLogs()
	rl := logs.ResourceLogs().AppendEmpty()
	sl := rl.ScopeLogs().AppendEmpty()
	lr := sl.LogRecords().AppendEmpty()
	lr.SetSeverityNumber(severity)
	lr.Body().SetStr(body)
	lr.Attributes().PutStr("request_id", "req-123")
	return &signals.LogSignal{
		Logs: logs,
	}
}

func TestRollupLogs(t *testing.T) {
	proc := New(config.RollupConfig{Enabled: true})
	proc.SetLevel(LevelCoarse)

	s1 := createLogSignal("Failed to connect to DB at 10.0.0.1", plog.SeverityNumberError)
	s2 := createLogSignal("Failed to connect to DB at 10.0.0.2", plog.SeverityNumberError)
	
	result := proc.RollupLogs([]*signals.LogSignal{s1, s2})

	// LevelCoarse truncates body to 20 chars + ...
	// Both "Failed to connect to DB at 10.0.0.1" and "...2" 
	// start with "Failed to connect to" (20 chars)
	// So they should match pattern and be aggregated.
	
	count := 0
	totalOccurrences := int64(0)
	rls := result.ResourceLogs()
	for i := 0; i < rls.Len(); i++ {
		sls := rls.At(i).ScopeLogs()
		for j := 0; j < sls.Len(); j++ {
			lrs := sls.At(j).LogRecords()
			count += lrs.Len()
			for k := 0; k < lrs.Len(); k++ {
				lr := lrs.At(k)
				occ, _ := lr.Attributes().Get("rollup.count")
				totalOccurrences += occ.Int()
				
				// request_id should be removed
				_, found := lr.Attributes().Get("request_id")
				assert.False(t, found)
			}
		}
	}
	
	assert.Equal(t, 1, count, "Logs should be aggregated into 1 pattern")
	assert.Equal(t, int64(2), totalOccurrences)
}
