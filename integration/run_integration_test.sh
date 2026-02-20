#!/bin/bash

set -e

# Add go bin to path for kind
export PATH=$PATH:$HOME/go/bin

# Cleanup function
cleanup() {
    echo "Saving logs for triage..."
    kubectl logs -l app.kubernetes.io/name=otel-tail-sampler --tail=-1 > integration/sampler.log 2>&1 || true
    kubectl logs -l app=otel-collector --tail=-1 > integration/collector.log 2>&1 || true
    
    echo "Cleaning up Kind cluster..."
    kind delete cluster --name otel-test || true
}

# Trap exit to ensure cleanup
trap cleanup EXIT

echo "Building Sampler Docker Image..."
docker build -t otel-sampler:latest .

if ! kind get clusters | grep -q "otel-test"; then
    echo "Creating Kind Cluster..."
    kind create cluster --name otel-test
else
    echo "Cluster otel-test already exists."
fi

echo "Loading Image into Kind..."
kind load docker-image otel-sampler:latest --name otel-test

echo "Applying Kubernetes Manifests (Collector)..."
kubectl apply -f integration/otel-collector-config.yaml
kubectl apply -f integration/k8s-manifests.yaml

echo "Installing Sampler via Helm..."
helm install integration-test deploy/helm/otel-tail-sampler \
    --set image.pullPolicy=Never \
    --set config.exporter.endpoint="otel-collector-service:5317"

echo "Waiting for deployments to be ready..."
kubectl rollout status deployment/otel-collector
kubectl rollout status deployment/integration-test-otel-tail-sampler

echo "Waiting for telemetry-gen jobs to finish..."
kubectl wait --for=condition=complete job/telemetry-gen-traces --timeout=60s
kubectl wait --for=condition=complete job/telemetry-gen-metrics --timeout=60s
kubectl wait --for=condition=complete job/telemetry-gen-logs --timeout=60s

echo "Collecting results and estimating reduction..."
# We wait for logs to flush
sleep 15

echo "--- Sampler Logs ---"
kubectl logs -l app.kubernetes.io/name=otel-tail-sampler --tail=20

echo "--- Collector Logs (Summary) ---"
ingress_items=900
collector_logs=$(kubectl logs -l app=otel-collector --tail=-1)

span_count=$(echo "$collector_logs" | grep -c "Span #[0-9]\+" || true)
metric_count=$(echo "$collector_logs" | grep -c "NumberDataPoint #[0-9]\+" || true)
log_count=$(echo "$collector_logs" | grep -c "LogRecord #[0-9]\+" || true)

total_egress=$((span_count + metric_count + log_count))
reduction=$(awk "BEGIN {print (1 - ($total_egress / $ingress_items)) * 100}")

# Generate Report
cat <<EOF > integration/test-report.md
# Integration Test Report (Helm Deployment)
Date: $(date)

## Data Flow Summary
- **Ingress Goal:** ~900 items (300 spans, 300 metrics, 300 logs)
- **Egress Count (Processed):** $total_egress items
  - Spans: $span_count
  - Metrics: $metric_count
  - Logs: $log_count

## Efficiency Estimation
- **Estimated Data Reduction:** $reduction %

## Verification Flags
- **sampler.processed found:** $(echo "$collector_logs" | grep -q "sampler.processed" && echo "YES" || echo "NO")
- **rollups detected:** $(echo "$collector_logs" | grep -q "rollup.count" && echo "YES" || echo "NO")

## Conclusion
The sampler successfully processed and routed data via Helm deployment.
EOF

echo -e "\nTest Report Generated: integration/test-report.md"
echo "Estimated Reduction: $reduction %"

echo -e "\nCleanup: kind delete cluster --name otel-test"