# Technical Architecture

The OTEL Tail Sampler is designed for high throughput and high availability. It operates as a middle-tier service between your applications (or an initial OTel Collector) and your long-term storage backend.

## Data Flow

1.  **Ingestion:** Data enters via gRPC OTLP.
2.  **Durability (WAL):** Every signal is immediately written to a high-speed Write-Ahead Log on disk.
3.  **Buffering:** Data is stored in **BigCache**, an off-heap in-memory store that avoids Go Garbage Collection overhead.
4.  **Evaluation:** The Decision Engine evaluates traces against configured policies.
5.  **Coordination:** If a node decides to sample, it broadcasts a **Protobuf-encoded decision** via the **Gossip Protocol**.
6.  **Processing:** All nodes in the cluster receive the decision and flush matching data from their local buffers.
7.  **Rollup:** Unsampled data is periodically aggregated by the Rollup Processor into high-level summaries.
8.  **Export:** Flagged data is exported to the final OTLP destination.

## Key Components

### Gossip Protocol (`memberlist`)
We use a SWIM-based gossip protocol to maintain cluster membership and propagate sampling decisions. This removes the need for a central database or coordinator, ensuring the sampler can scale horizontally with ease.

### BigCache Buffer
To handle millions of spans per second, we utilize BigCache. By serializing OTLP data into byte arrays and storing them off-heap, we maintain sub-millisecond response times even under heavy load, with zero GC pauses.

### Decision Engine
The engine supports multiple concurrent policies:
*   **Latency:** Sample if any span in a trace exceeds a threshold.
*   **Error:** Sample if any span contains an error status.
*   **Attribute:** Sample based on specific tags (e.g., `user_tier == "gold"`).

### Rollup Processor
A unique feature that ensures visibility even for unsampled data. It reduces cardinality by:
*   Aggregating metrics by name and common attributes.
*   Grouping logs by severity and body patterns (prefix matching).
