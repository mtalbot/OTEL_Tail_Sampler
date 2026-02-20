# Add go bin to path for kind
$env:PATH += ";$HOME\go\bin"

try {
    Write-Host "Building Sampler Docker Image..." -ForegroundColor Cyan
    docker build -t otel-sampler:latest .

    Write-Host "Creating Kind Cluster..." -ForegroundColor Cyan
    kind create cluster --name otel-test

    Write-Host "Loading Image into Kind..." -ForegroundColor Cyan
    kind load docker-image otel-sampler:latest --name otel-test

    Write-Host "Applying Kubernetes Manifests (Collector)..." -ForegroundColor Cyan
    kubectl apply -f integration/otel-collector-config.yaml
    kubectl apply -f integration/k8s-manifests.yaml

    Write-Host "Installing Sampler via Helm..." -ForegroundColor Cyan
    helm install integration-test deploy/helm/otel-tail-sampler `
        --set image.pullPolicy=Never `
        --set config.exporter.endpoint="otel-collector-service:5317"

    Write-Host "Waiting for deployments to be ready..." -ForegroundColor Cyan
    kubectl rollout status deployment/otel-collector
    kubectl rollout status deployment/integration-test-otel-tail-sampler

    Write-Host "Waiting for telemetry-gen jobs to finish..." -ForegroundColor Cyan
    kubectl wait --for=condition=complete job/telemetry-gen-traces --timeout=60s
    kubectl wait --for=condition=complete job/telemetry-gen-metrics --timeout=60s
    kubectl wait --for=condition=complete job/telemetry-gen-logs --timeout=60s

    Write-Host "Collecting results and estimating reduction..." -ForegroundColor Yellow
    # We wait a bit for logs to flush and rollups to potentially trigger
    Start-Sleep -Seconds 15

    Write-Host "--- Sampler Logs ---" -ForegroundColor Gray
    kubectl logs -l app.kubernetes.io/name=otel-tail-sampler --tail=20
    
    Write-Host "--- Collector Logs (Summary) ---" -ForegroundColor Gray
    $ingressItems = 900 # 30s * 10/s * 3 types
    $collectorLogs = kubectl logs -l app=otel-collector --tail=-1

    $spanCount = ($collectorLogs | Select-String "Span #[0-9]+" -AllMatches).Matches.Count
    $metricCount = ($collectorLogs | Select-String "NumberDataPoint #[0-9]+" -AllMatches).Matches.Count
    $logCount = ($collectorLogs | Select-String "LogRecord #[0-9]+" -AllMatches).Matches.Count

    $totalEgress = $spanCount + $metricCount + $logCount
    $reduction = 0
    if ($ingressItems -gt 0) {
        $reduction = [math]::Round((1 - ($totalEgress / $ingressItems)) * 100, 2)
    }

    # Generate Report
    $report = @"
# Integration Test Report (Helm Deployment)
Date: $(Get-Date)

## Data Flow Summary
- **Ingress Goal:** ~900 items (300 spans, 300 metrics, 300 logs)
- **Egress Count (Processed):** $totalEgress items
  - Spans: $spanCount
  - Metrics: $metricCount
  - Logs: $logCount

## Efficiency Estimation
- **Estimated Data Reduction:** $reduction %

## Verification Flags
- **sampler.processed found:** $(if ($collectorLogs -like "*sampler.processed*") { "YES" } else { "NO" })
- **rollups detected:** $(if ($collectorLogs -like "*rollup.count*") { "YES" } else { "NO" })

## Conclusion
$(if ($reduction -ge 0) { "The sampler successfully processed and routed data via Helm deployment." } else { "Warning: Data volume unexpected." })
"@

    $report | Out-File -FilePath "integration/test-report.md" -Encoding utf8

    Write-Host "`nTest Report Generated: integration/test-report.md" -ForegroundColor Green
    Write-Host "Estimated Reduction: $reduction %" -ForegroundColor Cyan
}
finally {
    Write-Host "`nSaving logs for triage..." -ForegroundColor Gray
    kubectl logs -l app.kubernetes.io/name=otel-tail-sampler --tail=-1 | Out-File -FilePath "integration/sampler.log" -Encoding utf8
    kubectl logs -l app=otel-collector --tail=-1 | Out-File -FilePath "integration/collector.log" -Encoding utf8

    Write-Host "Cleaning up Kind cluster..." -ForegroundColor Gray
    kind delete cluster --name otel-test
}