# OTEL Tail Sampler

Developed with ❤️ by **[MarsLabs](https://marslabs.example.com)**

A high-performance, distributed service for intelligent OpenTelemetry (OTEL) signal processing. It provides advanced **Tail Sampling** for traces and **Dynamic Cardinality Control** for metrics and logs.

## 🚀 Key Features

- **Distributed Tail Sampling:** Uses a gossip-based protocol (Protobuf over Memberlist) to coordinate sampling decisions across a cluster without a central coordinator.
- **Efficient Buffering:** Powered by **BigCache** for zero-GC overhead and a hybrid B-Tree/Map index for $O(\log N)$ time-range queries.
- **Dynamic Rollups:** Automatically aggregates metrics and logs during high-load periods or for unsampled data, ensuring 90%+ data reduction while maintaining visibility.
- **Reactive Aggregation:** Uses cache eviction hooks to ensure that expiring or overflowing data is rolled up rather than dropped.
- **Reliability:** Built-in high-speed Write-Ahead Log (WAL) for crash recovery and synchronous ACKs.
- **Cloud Native:** Packaged as a minimal distroless Docker image and deployable via Helm.

## 🏗 Architecture

The sampler sits between your applications and your long-term storage (e.g., Honeycomb, Datadog, or an OTel Collector).

1. **Ingress:** OTLP gRPC/HTTP Receiver.
2. **Persistence:** Immediate write to WAL.
3. **Storage:** Buffered in BigCache.
4. **Decision:** Traces evaluated against policies (latency, error, attributes).
5. **Gossip:** Decisions broadcasted to all peers.
6. **Egress:** Sampled data (with associated logs/metrics) is exported; unsampled data is rolled up and exported.

## 🛠 Development

### Prerequisites
- Go 1.24+
- Docker & Kind (for integration tests)
- Helm (for deployment testing)

### Build
```bash
go build ./...
```

### Run Tests
```bash
# Unit tests
go test -v ./internal/...

# Full system integration tests (in-memory)
go test -v ./tests/...
```

### Integration Testing (Kubernetes)
A script is provided to spin up a local `kind` cluster with an OTel Collector and telemetry generator:
```powershell
# Windows
powershell ./integration/run_integration_test.ps1

# Bash
./integration/run_integration_test.sh
```

## 📦 Deployment

### Docker
```bash
docker build -t otel-sampler:latest .
docker run -p 4317:4317 -e EXPORTER_ENDPOINT="collector:4317" otel-sampler:latest
```

### Helm
```bash
helm install my-sampler deploy/helm/otel-tail-sampler \
  --set config.exporter.endpoint="otel-collector:4317"
```

## 📄 License

This project is dual-licensed:
1. **Open Source Use:** Licensed under the [GNU Affero General Public License v3.0 (AGPL-3.0)](https://www.gnu.org/licenses/agpl-3.0.en.html).
2. **Proprietary/Commercial Use:** Requires a **Commercial Limited Site License**. 

See the [LICENSE](LICENSE) file for more details.
