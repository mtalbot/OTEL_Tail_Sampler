package tests

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"OTEL_Tail_Sampler/internal/buffer"
	"OTEL_Tail_Sampler/internal/config"
	"OTEL_Tail_Sampler/internal/decision"
	"OTEL_Tail_Sampler/internal/discovery"
	"OTEL_Tail_Sampler/internal/gossip"
	"OTEL_Tail_Sampler/internal/processor"
	"OTEL_Tail_Sampler/internal/receiver"
	"OTEL_Tail_Sampler/internal/rollup"
	"OTEL_Tail_Sampler/internal/wal"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/plog"
	"go.opentelemetry.io/collector/pdata/plog/plogotlp"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.opentelemetry.io/collector/pdata/pmetric/pmetricotlp"
	"go.opentelemetry.io/collector/pdata/ptrace"
	"go.opentelemetry.io/collector/pdata/ptrace/ptraceotlp"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func TestErrorSampling_ExportsContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	rcvPort := getFreePort(t)
	gspPort := getFreePort(t)

	// 1. Configure for Error Sampling ONLY
	cfg := config.DefaultConfig()
	cfg.Receiver.GRPCPort = rcvPort
	cfg.Gossip.Port = gspPort
	cfg.Buffer.TTLSeconds = 10
	cfg.Buffer.Size = 10
	cfg.Decision.Policies = []config.PolicyConfig{
		{Name: "catch-errors", Type: "error"},
	}
	// Disable periodic rollup to ensure we are testing the immediate decision path
	cfg.Rollup.Enabled = false 

	tmpDir, err := os.MkdirTemp("", "error-test-wal")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	walLog, _ := wal.Open(tmpDir)
	defer walLog.Close()

	buf := buffer.New(cfg.Buffer, true)
	disc, _ := discovery.New(cfg.Discovery)
	gsp := gossip.New(cfg.Gossip, disc, true)
	engine := decision.New(cfg.Decision, gsp, true)
	rlp := rollup.New(cfg.Rollup)
	mockExp := &MockExporter{}
	// Pass nil telemetry for simplicity
	proc := processor.New(buf, gsp, rlp, mockExp, true, nil)

	require.NoError(t, buf.Start(ctx))
	require.NoError(t, gsp.Start(ctx))
	proc.Start(ctx)

	bridge := &consumerBridge{buffer: buf, decision: engine, wal: walLog}
	rcv := receiver.New(cfg.Receiver, bridge, true, nil)
	require.NoError(t, rcv.Start(ctx))
	defer rcv.Stop()

	// Wait for startup
	time.Sleep(200 * time.Millisecond)

	conn, err := grpc.NewClient(fmt.Sprintf("127.0.0.1:%d", rcvPort), grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	defer conn.Close()

	traceClient := ptraceotlp.NewGRPCClient(conn)
	metricClient := pmetricotlp.NewGRPCClient(conn)
	logClient := plogotlp.NewGRPCClient(conn)
	
	now := time.Now()

	// 2. Send Contextual Data (Metrics/Logs) BEFORE the trace
	// These should be buffered and picked up when the error trace arrives.
	
	// Metric at T-1s
	met := pmetric.NewMetrics()
	mt := met.ResourceMetrics().AppendEmpty().ScopeMetrics().AppendEmpty().Metrics().AppendEmpty()
	mt.SetName("cpu_usage")
	dp := mt.SetEmptySum().DataPoints().AppendEmpty()
	dp.SetIntValue(85)
	dp.SetTimestamp(pcommon.NewTimestampFromTime(now.Add(-1 * time.Second)))
	_, err = metricClient.Export(ctx, pmetricotlp.NewExportRequestFromMetrics(met))
	assert.NoError(t, err)
	
	// Log at T-0.5s
	lg := plog.NewLogs()
	lrec := lg.ResourceLogs().AppendEmpty().ScopeLogs().AppendEmpty().LogRecords().AppendEmpty()
	lrec.Body().SetStr("database connection timeout")
	lrec.SetSeverityText("ERROR")
	lrec.SetTimestamp(pcommon.NewTimestampFromTime(now.Add(-500 * time.Millisecond)))
	_, err = logClient.Export(ctx, plogotlp.NewExportRequestFromLogs(lg))
	assert.NoError(t, err)

	time.Sleep(100 * time.Millisecond) // Ensure buffered

	// 3. Send SUCCESS Trace (Should NOT be sampled)
	trOk := ptrace.NewTraces()
	spanOk := trOk.ResourceSpans().AppendEmpty().ScopeSpans().AppendEmpty().Spans().AppendEmpty()
	spanOk.SetName("health-check")
	spanOk.SetTraceID([16]byte{1})
	spanOk.SetSpanID([8]byte{1})
	spanOk.Status().SetCode(ptrace.StatusCodeOk)
	spanOk.SetStartTimestamp(pcommon.NewTimestampFromTime(now.Add(-2 * time.Second)))
	spanOk.SetEndTimestamp(pcommon.NewTimestampFromTime(now.Add(-1 * time.Second)))
	
	_, err = traceClient.Export(ctx, ptraceotlp.NewExportRequestFromTraces(trOk))
	assert.NoError(t, err)

	// Verify NOTHING exported yet
	time.Sleep(500 * time.Millisecond)
	mockExp.mu.Lock()
	assert.Len(t, mockExp.Traces, 0, "Success trace should not be sampled")
	mockExp.mu.Unlock()

	// 4. Send ERROR Trace (Should TRIGGER sampling)
	trErr := ptrace.NewTraces()
	spanErr := trErr.ResourceSpans().AppendEmpty().ScopeSpans().AppendEmpty().Spans().AppendEmpty()
	spanErr.SetName("failed-checkout")
	spanErr.SetTraceID([16]byte{2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2})
	spanErr.SetSpanID([8]byte{2})
	spanErr.Status().SetCode(ptrace.StatusCodeError) // <-- TRIGGER
	// Trace duration covers the metrics/logs
	spanErr.SetStartTimestamp(pcommon.NewTimestampFromTime(now.Add(-2 * time.Second)))
	spanErr.SetEndTimestamp(pcommon.NewTimestampFromTime(now))
	
	_, err = traceClient.Export(ctx, ptraceotlp.NewExportRequestFromTraces(trErr))
	assert.NoError(t, err)

	// 5. Verify EVERYTHING Exported (Trace + Context)
	require.Eventually(t, func() bool {
		mockExp.mu.Lock()
		defer mockExp.mu.Unlock()
		// We expect 1 trace, 1 metric batch, 1 log batch
		return len(mockExp.Traces) == 1 && len(mockExp.Metrics) == 1 && len(mockExp.Logs) == 1
	}, 5*time.Second, 100*time.Millisecond, "Error trace and context should be exported")

	mockExp.mu.Lock()
	defer mockExp.mu.Unlock()

	// Verify it is indeed the error trace
	assert.Equal(t, "failed-checkout", mockExp.Traces[0].ResourceSpans().At(0).ScopeSpans().At(0).Spans().At(0).Name())
	
	// Verify we got the metric
	assert.Equal(t, "cpu_usage", mockExp.Metrics[0].ResourceMetrics().At(0).ScopeMetrics().At(0).Metrics().At(0).Name())
	
	// Verify we got the log
	assert.Equal(t, "database connection timeout", mockExp.Logs[0].ResourceLogs().At(0).ScopeLogs().At(0).LogRecords().At(0).Body().AsString())
}
