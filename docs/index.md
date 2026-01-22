# OTEL Tail Sampler: Intelligent Observability at Scale

**Developed by [MarsLabs](https://marslabs.example.com)**

**The High-Performance, Distributed Solution for Tail Sampling and Cardinality Control.**

In modern distributed systems, data volume is your greatest asset and your biggest liability. Traditional sampling drops the very traces you need most—the errors, the outliers, and the bottlenecks. 

**OTEL Tail Sampler** changes the game. By buffering data and coordinating decisions across your entire cluster, we ensure you keep the 1% of data that matters, while intelligently rolling up the 99% that doesn't.

---

## 🚀 Key Benefits

### 🎯 Never Miss a Performance Outlier
Our distributed tail sampling ensures that if a trace is interesting (errors, high latency, or specific business attributes), the *entire* trace is captured across all nodes, along with its associated logs and fine-grained metrics.

### 💰 Massive Cost Reduction
Stop paying for redundant data. Our dynamic rollup processor aggregates unsampled metrics and logs into high-level summaries, providing 90%+ data reduction while maintaining total visibility.

### 🛡️ Enterprise-Grade Reliability
With a high-performance Write-Ahead Log (WAL) and BigCache-backed in-memory storage, your data is safe from crashes and your service stays fast with zero garbage collection overhead.

---

## 🛠 Features for Engineers

- **Protobuf-Powered Gossip:** Low-latency cluster coordination without a central coordinator.
- **Zero-GC Architecture:** Uses BigCache for high-throughput buffering without performance degradation.
- **Contextual Correlation:** Automatically links logs and metrics to sampled traces within the same time window.
- **OTLP Native:** Built on OpenTelemetry standards for seamless integration with your existing stack.

---

## 🏁 Get Started

Ready to optimize your observability pipeline?

[View Getting Started Guide](./getting-started/quickstart.md) | [Technical Architecture](./architecture/overview.md) | [Configuration Reference](./configuration/reference.md)
