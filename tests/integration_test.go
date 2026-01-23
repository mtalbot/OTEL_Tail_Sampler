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

func TestFullSystemIntegration(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	rcvPort := getFreePort(t)
	gspPort := getFreePort(t)

	cfg := config.DefaultConfig()
	cfg.Receiver.GRPCPort = rcvPort
	cfg.Gossip.Port = gspPort
	cfg.Buffer.TTLSeconds = 10
	cfg.Buffer.Size = 10
	cfg.Decision.Policies = []config.PolicyConfig{
		{Name: "all", Type: "latency", LatencyThresholdMS: -1},
	}

	tmpDir, err := os.MkdirTemp("", "full-system-wal")
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
	proc := processor.New(buf, gsp, rlp, mockExp, true, nil)

	require.NoError(t, buf.Start(ctx))
	require.NoError(t, gsp.Start(ctx))
	proc.Start(ctx)

	bridge := &consumerBridge{buffer: buf, decision: engine, wal: walLog}
	rcv := receiver.New(cfg.Receiver, bridge, true, nil)
	require.NoError(t, rcv.Start(ctx))
	defer rcv.Stop()

	time.Sleep(200 * time.Millisecond)

	conn, err := grpc.NewClient(fmt.Sprintf("127.0.0.1:%d", rcvPort), grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	defer conn.Close()

	traceClient := ptraceotlp.NewGRPCClient(conn)
	metricClient := pmetricotlp.NewGRPCClient(conn)
	logClient := plogotlp.NewGRPCClient(conn)
	
	now := time.Now()

	// 1. Metric
	met := pmetric.NewMetrics()
	mt := met.ResourceMetrics().AppendEmpty().ScopeMetrics().AppendEmpty().Metrics().AppendEmpty()
	mt.SetName("test-metric")
	dp := mt.SetEmptySum().DataPoints().AppendEmpty()
	dp.SetIntValue(100)
	dp.SetTimestamp(pcommon.NewTimestampFromTime(now))
	_, err = metricClient.Export(ctx, pmetricotlp.NewExportRequestFromMetrics(met))
	assert.NoError(t, err)
	
	// 2. Log
	lg := plog.NewLogs()
	lrec := lg.ResourceLogs().AppendEmpty().ScopeLogs().AppendEmpty().LogRecords().AppendEmpty()
	lrec.Body().SetStr("test-log")
	lrec.SetTimestamp(pcommon.NewTimestampFromTime(now))
	_, err = logClient.Export(ctx, plogotlp.NewExportRequestFromLogs(lg))
	assert.NoError(t, err)

	time.Sleep(200 * time.Millisecond)

	// 3. Trace
	tr := ptrace.NewTraces()
	span := tr.ResourceSpans().AppendEmpty().ScopeSpans().AppendEmpty().Spans().AppendEmpty()
	span.SetName("test-span")
	span.SetTraceID([16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16})
	span.SetSpanID([8]byte{1, 2, 3, 4, 5, 6, 7, 8})
	span.SetStartTimestamp(pcommon.NewTimestampFromTime(now.Add(-100 * time.Millisecond)))
	span.SetEndTimestamp(pcommon.NewTimestampFromTime(now))
	
	_, err = traceClient.Export(ctx, ptraceotlp.NewExportRequestFromTraces(tr))
	assert.NoError(t, err)

	require.Eventually(t, func() bool {
		mockExp.mu.Lock()
		defer mockExp.mu.Unlock()
		return len(mockExp.Traces) > 0 && len(mockExp.Metrics) > 0 && len(mockExp.Logs) > 0
	}, 10*time.Second, 100*time.Millisecond, "All data types should reach exporter")

	assert.Equal(t, "test-span", mockExp.Traces[0].ResourceSpans().At(0).ScopeSpans().At(0).Spans().At(0).Name())
	assert.Equal(t, "test-metric", mockExp.Metrics[0].ResourceMetrics().At(0).ScopeMetrics().At(0).Metrics().At(0).Name())
	assert.Equal(t, "test-log", mockExp.Logs[0].ResourceLogs().At(0).ScopeLogs().At(0).LogRecords().At(0).Body().AsString())
	
	val, ok := mockExp.Traces[0].ResourceSpans().At(0).Resource().Attributes().Get("sampler.processed")
	assert.True(t, ok)
	assert.True(t, val.Bool())
}