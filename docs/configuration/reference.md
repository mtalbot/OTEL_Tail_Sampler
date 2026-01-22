# Configuration Reference

The sampler is configured via a YAML file or environment variables.

## Top-Level Options

| Option | Description | Default |
| :--- | :--- | :--- |
| `debug` | Enable verbose debug logging. | `true` |

## Receiver Settings (`receiver`)

| Option | Description | Default |
| :--- | :--- | :--- |
| `grpc_port` | Port for OTLP gRPC ingestion. | `4317` |
| `host` | Host address to bind to. | `0.0.0.0` |

## Buffer Settings (`buffer`)

| Option | Description | Default |
| :--- | :--- | :--- |
| `size` | Maximum cache size in MB. | `512` |
| `ttl_seconds` | How long to buffer data before decision or rollup. | `30` |
| `max_traces` | Maximum number of trace entries to index. | `5000` |

## Gossip Settings (`gossip`)

| Option | Description | Default |
| :--- | :--- | :--- |
| `port` | Port for cluster coordination. | `7946` |
| `bind_addr` | IP to bind for gossip. | `0.0.0.0` |
| `interval` | Gossip message interval in ms. | `100` |

## Decision Policies (`decision.policies`)

Define a list of triggers.

```yaml
decision:
  policies:
    - name: "slow-traces"
      type: "latency"
      latency_threshold_ms: 500
    - name: "errors"
      type: "error"
    - name: "vip-customers"
      type: "attribute"
      attribute_key: "customer.type"
      attribute_value: "enterprise"
```

## Exporter Settings (`exporter`)

| Option | Description | Default |
| :--- | :--- | :--- |
| `endpoint` | Downstream OTLP gRPC endpoint. | `localhost:4317` |
| `insecure` | Use insecure connection. | `true` |
