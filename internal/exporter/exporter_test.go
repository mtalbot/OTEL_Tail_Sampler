package exporter

import (
	"context"
	"fmt"
	"net"
	"testing"
	"time"

	"OTEL_Tail_Sampler/internal/config"
	"OTEL_Tail_Sampler/pkg/signals"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/pdata/plog"
	"go.opentelemetry.io/collector/pdata/plog/plogotlp"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.opentelemetry.io/collector/pdata/pmetric/pmetricotlp"
	"go.opentelemetry.io/collector/pdata/ptrace"
	"go.opentelemetry.io/collector/pdata/ptrace/ptraceotlp"
	"google.golang.org/grpc"
)

type mockTraceServer struct {
	ptraceotlp.UnimplementedGRPCServer
	received []ptrace.Traces
}

func (m *mockTraceServer) Export(ctx context.Context, req ptraceotlp.ExportRequest) (ptraceotlp.ExportResponse, error) {
	m.received = append(m.received, req.Traces())
	return ptraceotlp.NewExportResponse(), nil
}

type mockMetricServer struct {
	pmetricotlp.UnimplementedGRPCServer
	received []pmetric.Metrics
}

func (m *mockMetricServer) Export(ctx context.Context, req pmetricotlp.ExportRequest) (pmetricotlp.ExportResponse, error) {
	m.received = append(m.received, req.Metrics())
	return pmetricotlp.NewExportResponse(), nil
}

type mockLogServer struct {
	plogotlp.UnimplementedGRPCServer
	received []plog.Logs
}

func (m *mockLogServer) Export(ctx context.Context, req plogotlp.ExportRequest) (plogotlp.ExportResponse, error) {
	m.received = append(m.received, req.Logs())
	return plogotlp.NewExportResponse(), nil
}

func TestExporter_Integration(t *testing.T) {
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	port := lis.Addr().(*net.TCPAddr).Port
	
	s := grpc.NewServer()
	traceSrv := &mockTraceServer{}
	metricSrv := &mockMetricServer{}
	logSrv := &mockLogServer{}
	
	ptraceotlp.RegisterGRPCServer(s, traceSrv)
	pmetricotlp.RegisterGRPCServer(s, metricSrv)
	plogotlp.RegisterGRPCServer(s, logSrv)
	
	go s.Serve(lis)
	defer s.Stop()

	// Create Exporter
	cfg := config.ExporterConfig{
		Endpoint: fmt.Sprintf("127.0.0.1:%d", port),
		Insecure: true,
	}
	exp, err := New(cfg)
	require.NoError(t, err)
	defer exp.Stop()

	// 1. Test Trace Export
	traces := ptrace.NewTraces()
	traces.ResourceSpans().AppendEmpty().ScopeSpans().AppendEmpty().Spans().AppendEmpty().SetName("test")
	err = exp.ExportTrace(&signals.TraceSignal{Traces: traces})
	assert.NoError(t, err)

	// 2. Test Metrics Export
	metrics := pmetric.NewMetrics()
	metrics.ResourceMetrics().AppendEmpty().ScopeMetrics().AppendEmpty().Metrics().AppendEmpty().SetName("test_metric")
	err = exp.ExportMetrics(metrics)
	assert.NoError(t, err)

	// 3. Test Logs Export
	logs := plog.NewLogs()
	logs.ResourceLogs().AppendEmpty().ScopeLogs().AppendEmpty().LogRecords().AppendEmpty().Body().SetStr("test_log")
	err = exp.ExportLogs(logs)
	assert.NoError(t, err)

	// Verify
	require.Eventually(t, func() bool {
		return len(traceSrv.received) == 1 && 
			   len(metricSrv.received) == 1 && 
			   len(logSrv.received) == 1
	}, 2*time.Second, 100*time.Millisecond)
}