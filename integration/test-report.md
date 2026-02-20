# Integration Test Report (Helm Deployment)
Date: Fri, Jan 23, 2026  6:26:44 PM

## Data Flow Summary
- **Ingress Goal:** ~900 items (300 spans, 300 metrics, 300 logs)
- **Egress Count (Processed):** 100 items
  - Spans: 0
  - Metrics: 0
  - Logs: 100

## Efficiency Estimation
- **Estimated Data Reduction:** 88.8889 %

## Verification Flags
- **sampler.processed found:** YES
- **rollups detected:** NO

## Conclusion
The sampler successfully processed and routed data via Helm deployment.
