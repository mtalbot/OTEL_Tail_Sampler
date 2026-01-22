package exporter

import (
	"context"
	"fmt"
	"time"

	"OTEL_Tail_Sampler/internal/config"
	"OTEL_Tail_Sampler/pkg/signals"

	"go.opentelemetry.io/collector/pdata/plog"
	"go.opentelemetry.io/collector/pdata/plog/plogotlp"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.opentelemetry.io/collector/pdata/pmetric/pmetricotlp"
	"go.opentelemetry.io/collector/pdata/ptrace/ptraceotlp"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type OTLPExporter struct {
	cfg        config.ExporterConfig
	conn       *grpc.ClientConn
	traceClient  ptraceotlp.GRPCClient
	metricClient pmetricotlp.GRPCClient
	logClient    plogotlp.GRPCClient
}

func New(cfg config.ExporterConfig) (*OTLPExporter, error) {
	opts := []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()), // Prototype default
	}
	
	// TODO: Support TLS if configured

	conn, err := grpc.NewClient(cfg.Endpoint, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create gRPC client: %w", err)
	}

	return &OTLPExporter{
		cfg:          cfg,
		conn:         conn,
		traceClient:  ptraceotlp.NewGRPCClient(conn),
		metricClient: pmetricotlp.NewGRPCClient(conn),
		logClient:    plogotlp.NewGRPCClient(conn),
	}, nil
}

func (e *OTLPExporter) Stop() {
	if e.conn != nil {
		e.conn.Close()
	}
}

func (e *OTLPExporter) ExportTrace(sig *signals.TraceSignal) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req := ptraceotlp.NewExportRequestFromTraces(sig.Traces)
	_, err := e.traceClient.Export(ctx, req)
	if err != nil {
		return fmt.Errorf("failed to export traces: %w", err)
	}
	return nil
}

func (e *OTLPExporter) ExportMetrics(metrics pmetric.Metrics) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req := pmetricotlp.NewExportRequestFromMetrics(metrics)
	_, err := e.metricClient.Export(ctx, req)
	if err != nil {
		return fmt.Errorf("failed to export metrics: %w", err)
	}
	return nil
}

func (e *OTLPExporter) ExportLogs(logs plog.Logs) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req := plogotlp.NewExportRequestFromLogs(logs)
	_, err := e.logClient.Export(ctx, req)
	if err != nil {
		return fmt.Errorf("failed to export logs: %w", err)
	}
	return nil
}
