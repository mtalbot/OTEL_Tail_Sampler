# Project Planning: OTEL_Tail_Sampler

## 1. Project Overview
A service to collect, buffer, and then either tail sample or provide rollups. Supports all key OpenTelemetry (OTEL) signals (Traces, Metrics, Logs). It will use a distributed signalling method to determine the tail sampling to avoid the need for all signals to be routed to the same node. Various different signal triggers will be configurable to allow the decision to sample a trace, but also will be used to dynamically control the cardinality of metrics and logs.

## 2. Goals
- Efficiently buffer OTEL signals.
- Implement distributed tail sampling logic.
- Provide rollup capabilities of metrics and logs.
- Provide a mechanism to decide when a sampling policy should be applied.
- Provide a mechanism to decide on the cardinality of metrics and logs.
- Ensure high performance and low overhead.
- Have a method to discover other instances running for distributed tail sampling.
- Have various levels of delivery guarantees configurable.

## 3. Architecture & Design

- **Language:** Go
- **Key Components:**
    - **Collector/Receiver:** Ingests OTLP data.
    - **Buffer Manager:** Temporary storage (ring buffer) waiting for sampling decisions.
    - **Discovery Service:** DNS-based discovery of peer nodes.
    - **Gossip Engine:** Propagates sampling decisions and cluster state across nodes.
    - **Sampling Decision Engine:** Evaluates triggers to decide on sampling or rolling up.
    - **Rollup Processor:** Handles cardinality reduction for metrics and logs based on engine decisions.
    - **Exporter:** Sends processed data to downstream OTLP backends.

## 4. Roadmap & Tasks

### Phase 1: Core Foundation & Ingestion
- [x] **Project Setup**
    - [x] Initialize Go module and directory structure.
    - [x] Setup logging and basic configuration framework (viper/koanf etc).
    - [x] Define internal data models for Signals.
- [x] **OTLP Receiver**
    - [x] Implement gRPC OTLP Receiver for Traces.
    - [x] Implement gRPC OTLP Receiver for Metrics.
    - [x] Implement gRPC OTLP Receiver for Logs.
    - [x] Verify basic ingestion with acceptance tests.

### Phase 2: Buffering & Storage
- [x] **Buffer Implementation**
    - [x] Design interface for Buffer Manager.
    - [x] Implement In-Memory Ring Buffer (partitioned by TraceID/Resource).
    - [x] Implement Time-based eviction policies (TTL).
    - [x] Add capacity monitoring and overflow handling.

### Phase 3: Discovery & Distributed Clustering
- [x] **Node Discovery**
    - [x] Implement DNS-based peer discovery mechanism.
    - [x] Implement static peer list for local dev/testing.
- [x] **Gossip Protocol**
    - [x] Integrate Gossip library (e.g., memberlist).
    - [x] Define message format for "Sampling Decisions" (using Protobuf).
    - [x] Implement broadcast mechanism for triggers.

### Phase 4: Sampling Logic & Decision Engine
- [x] **Trigger Configuration**
    - [x] Define configuration schema for sampling policies (latency, error rate, specific attributes).
    - [x] Implement hot-reload for policy configs.
- [x] **Decision Engine**
    - [x] Implement local evaluation of triggers.
    - [x] Implement distributed decision listener (from Gossip).
    - [x] Wire Buffer Manager to flush/drop based on decisions.

### Phase 5: Rollups & Cardinality Control
- [ ] **Rollup Processor**
    - [ ] Implement Metric aggregation logic (sum, count, histogram buckets).
    - [ ] Implement Log pattern recognition/grouping (remove dynamic values).
- [ ] **Dynamic Control**
    - [ ] Connect Decision Engine to Rollup Processor (e.g., "High Load" trigger -> "Aggressive Rollup").

### Phase 6: Export & Reliability
- [x] **OTLP Exporter**
    - [x] Implement OTLP gRPC Exporter.
    - [x] Implement connection pooling/management.
- [x] **Delivery Guarantees**
    - [x] Implement configurable ACK mechanisms.
    - [x] Implement local persistent disk buffer for failure scenarios (WAL).

## 5. Next Steps
1.  Verify the full flow using end-to-end integration tests.
2.  Perform a Docker build and run a containerized instance.
3.  Add documentation for configuration.