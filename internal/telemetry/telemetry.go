package telemetry

import (
	"context"
	"fmt"
	"time"

	"OTEL_Tail_Sampler/internal/config"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.24.0"
)

type Telemetry struct {
	meter metric.Meter
	
	// Instruments
	SignalsReceived metric.Int64Counter
	TracesSampled   metric.Int64Counter
	RollupsTotal    metric.Int64Counter
	BufferItems     metric.Int64ObservableGauge
	GossipPeers     metric.Int64ObservableGauge
}

func Init(ctx context.Context, cfg config.ExporterConfig) (*Telemetry, func(), error) {
	// 1. Create OTLP Exporter
	exporter, err := otlpmetricgrpc.New(ctx,
		otlpmetricgrpc.WithEndpoint(cfg.Endpoint),
		otlpmetricgrpc.WithInsecure(), // Match main exporter for now
	)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create metric exporter: %w", err)
	}

	// 2. Resource
	res, err := resource.Merge(
		resource.Default(),
		resource.NewWithAttributes(
			semconv.SchemaURL,
			semconv.ServiceName("otel-tail-sampler"),
			semconv.ServiceVersion("1.0.0"),
		),
	)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create resource: %w", err)
	}

	// 3. Provider
	provider := sdkmetric.NewMeterProvider(
		sdkmetric.WithResource(res),
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(exporter, sdkmetric.WithInterval(10*time.Second))),
	)
	otel.SetMeterProvider(provider)

	// 4. Initialize Instruments
	meter := provider.Meter("otel-tail-sampler-internal")
	t := &Telemetry{meter: meter}

	t.SignalsReceived, _ = meter.Int64Counter("sampler.signals_received_total",
		metric.WithDescription("Total number of OTLP signals received"),
	)
	t.TracesSampled, _ = meter.Int64Counter("sampler.traces_sampled_total",
		metric.WithDescription("Total number of traces sampled (kept)"),
	)
	t.RollupsTotal, _ = meter.Int64Counter("sampler.rollups_total",
		metric.WithDescription("Total number of signals aggregated via rollups"),
	)

	// Shutdown function
	shutdown := func() {
		provider.Shutdown(context.Background())
	}

	return t, shutdown, nil
}

func (t *Telemetry) RegisterBufferGauge(fn func(context.Context, metric.Int64Observer) error) error {
	var err error
	t.BufferItems, err = t.meter.Int64ObservableGauge("sampler.buffer_items",
		metric.WithDescription("Current number of items in the local buffer"),
		metric.WithInt64Callback(fn),
	)
	return err
}

func (t *Telemetry) RegisterGossipGauge(fn func(context.Context, metric.Int64Observer) error) error {
	var err error
	t.GossipPeers, err = t.meter.Int64ObservableGauge("sampler.gossip_peers",
		metric.WithDescription("Current number of peers in the gossip cluster"),
		metric.WithInt64Callback(fn),
	)
	return err
}

func (t *Telemetry) Meter() metric.Meter {
	return t.meter
}
