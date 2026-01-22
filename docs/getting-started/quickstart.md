# Quickstart Guide

Get the OTEL Tail Sampler up and running in less than 5 minutes.

## 1. Prerequisites
- Docker installed
- Kubernetes cluster (optional, for Helm)
- `helm` (optional)

## 2. Running with Docker

The easiest way to try the sampler is using our pre-built image.

```bash
docker run -p 4317:4317 -p 7946:7946 \
  -e EXPORTER_ENDPOINT="your-backend:4317" \
  otel-sampler:latest
```

## 3. Deploying with Helm

For production-like environments, use the provided Helm chart.

```bash
# Clone the repo
git clone https://github.com/mtalbot/OTEL_Tail_Sampler.git
cd OTEL_Tail_Sampler

# Install the chart
helm install sampler deploy/helm/otel-tail-sampler \
  --set config.exporter.endpoint="otel-collector:4317"
```

## 4. Verify Data Flow

Send a test trace using `telemetrygen`:

```bash
docker run ghcr.io/open-telemetry/opentelemetry-collector-contrib/telemetrygen:latest \
  traces --otlp-endpoint=localhost:4317 --otlp-insecure --rate=1 --duration=5s
```

Check the logs:
```bash
# If running in Docker
docker logs <container_id> | grep "Sampling trace"
```

## 5. Next Steps
- Explore [Configuration Reference](../configuration/reference.md) to define your sampling policies.
- Learn about the [Technical Architecture](../architecture/overview.md).

