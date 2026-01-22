package receiver

import (
	"context"
	"fmt"
	"log"
	"net"
	"time"

	"OTEL_Tail_Sampler/internal/config"
	"OTEL_Tail_Sampler/pkg/signals"

	"go.opentelemetry.io/collector/pdata/plog/plogotlp"
	"go.opentelemetry.io/collector/pdata/pmetric/pmetricotlp"
	"go.opentelemetry.io/collector/pdata/ptrace/ptraceotlp"
	"google.golang.org/grpc"
)

// Consumer is an interface for components that consume signals
type Consumer interface {
	Consume(signal signals.Signal) error
}

// Receiver handles incoming OTLP data
type Receiver struct {
	cfg        config.ReceiverConfig
	consumer   Consumer
	grpcServer *grpc.Server
	debug      bool
	
	// Implementations of the gRPC services
	traceServer  *traceReceiver
	metricServer *metricReceiver
	logServer    *logReceiver
}

// New creates a new Receiver
func New(cfg config.ReceiverConfig, consumer Consumer, debug bool) *Receiver {
	return &Receiver{
		cfg:      cfg,
		consumer: consumer,
		debug:    debug,
	}
}

func (r *Receiver) log(format string, v ...interface{}) {
	if r.debug {
		log.Printf("[RECEIVER] "+format, v...)
	}
}

// Start starts the gRPC server
func (r *Receiver) Start(ctx context.Context) error {
	addr := fmt.Sprintf("%s:%d", r.cfg.Host, r.cfg.GRPCPort)
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("failed to listen: %w", err)
	}

	r.grpcServer = grpc.NewServer()

	r.traceServer = &traceReceiver{consumer: r.consumer, parent: r}
	ptraceotlp.RegisterGRPCServer(r.grpcServer, r.traceServer)

	r.metricServer = &metricReceiver{consumer: r.consumer, parent: r}
	pmetricotlp.RegisterGRPCServer(r.grpcServer, r.metricServer)

	r.logServer = &logReceiver{consumer: r.consumer, parent: r}
	plogotlp.RegisterGRPCServer(r.grpcServer, r.logServer)

	log.Printf("Receiver listening on %s", addr)

	go func() {
		if err := r.grpcServer.Serve(lis); err != nil {
			log.Printf("gRPC server error: %v", err)
		}
	}()

	return nil
}

// Stop stops the receiver
func (r *Receiver) Stop() {
	if r.grpcServer != nil {
		r.grpcServer.GracefulStop()
	}
}

// traceReceiver implements ptraceotlp.GRPCServer
type traceReceiver struct {
	ptraceotlp.UnimplementedGRPCServer
	consumer Consumer
	parent   *Receiver
}

func (tr *traceReceiver) Export(ctx context.Context, req ptraceotlp.ExportRequest) (ptraceotlp.ExportResponse, error) {
	traces := req.Traces()
	tr.parent.log("Received trace batch: %d spans", traces.SpanCount())
	// Create internal signal
	sig := &signals.TraceSignal{
		Traces: traces,
		Ctx: signals.Context{
			ReceivedAt: timeNow(),
			// TODO: Extract source from ctx
		},
	}
	if err := tr.consumer.Consume(sig); err != nil {
		return ptraceotlp.NewExportResponse(), err
	}
	return ptraceotlp.NewExportResponse(), nil
}

// metricReceiver implements pmetricotlp.GRPCServer
type metricReceiver struct {
	pmetricotlp.UnimplementedGRPCServer
	consumer Consumer
	parent   *Receiver
}

func (mr *metricReceiver) Export(ctx context.Context, req pmetricotlp.ExportRequest) (pmetricotlp.ExportResponse, error) {
	metrics := req.Metrics()
	mr.parent.log("Received metric batch: %d data points", metrics.DataPointCount())
	sig := &signals.MetricSignal{
		Metrics: metrics,
		Ctx: signals.Context{
			ReceivedAt: timeNow(),
		},
	}
	if err := mr.consumer.Consume(sig); err != nil {
		return pmetricotlp.NewExportResponse(), err
	}
	return pmetricotlp.NewExportResponse(), nil
}

// logReceiver implements plogotlp.GRPCServer
type logReceiver struct {
	plogotlp.UnimplementedGRPCServer
	consumer Consumer
	parent   *Receiver
}

func (lr *logReceiver) Export(ctx context.Context, req plogotlp.ExportRequest) (plogotlp.ExportResponse, error) {
	logs := req.Logs()
	lr.parent.log("Received log batch: %d records", logs.LogRecordCount())
	sig := &signals.LogSignal{
		Logs: logs,
		Ctx: signals.Context{
			ReceivedAt: timeNow(),
		},
	}
	if err := lr.consumer.Consume(sig); err != nil {
		return plogotlp.NewExportResponse(), err
	}
	return plogotlp.NewExportResponse(), nil
}

// Helper to allow mocking time if needed, though simpler to just use time.Now()
func timeNow() time.Time {
	return time.Now()
}
