package config

// Config holds the global configuration for the application
type Config struct {
	Debug     bool            `mapstructure:"debug"`
	Receiver  ReceiverConfig  `mapstructure:"receiver"`
	Buffer    BufferConfig    `mapstructure:"buffer"`
	Discovery DiscoveryConfig `mapstructure:"discovery"`
	Gossip    GossipConfig    `mapstructure:"gossip"`
	Decision  DecisionConfig  `mapstructure:"decision"`
	Rollup    RollupConfig    `mapstructure:"rollup"`
	Exporter  ExporterConfig  `mapstructure:"exporter"`
}

type ReceiverConfig struct {
	GRPCPort int    `mapstructure:"grpc_port"`
	HTTPPort int    `mapstructure:"http_port"`
	Host     string `mapstructure:"host"`
}

type BufferConfig struct {
	Size        int `mapstructure:"size"`
	TTLSeconds  int `mapstructure:"ttl_seconds"`
	MaxTraces   int `mapstructure:"max_traces"`
}

type DiscoveryConfig struct {
	Type        string   `mapstructure:"type"` // "dns" or "static"
	ServiceName string   `mapstructure:"service_name"`
	Peers       []string `mapstructure:"peers"` // For static discovery
}

type GossipConfig struct {
	Port         int    `mapstructure:"port"`
	BindAddr     string `mapstructure:"bind_addr"`
	Interval     int    `mapstructure:"interval"` // in milliseconds
}

type DecisionConfig struct {
	Policies []PolicyConfig `mapstructure:"policies"`
}

type PolicyConfig struct {
	Name string `mapstructure:"name"`
	Type string `mapstructure:"type"` // "latency", "error", "attribute", "probabilistic"

	// Latency
	LatencyThresholdMS int64 `mapstructure:"latency_threshold_ms"`

	// Attribute
	AttributeKey   string `mapstructure:"attribute_key"`
	AttributeValue string `mapstructure:"attribute_value"`

	// Probabilistic
	SampleRate float64 `mapstructure:"sample_rate"` // 0.0 to 1.0
}

type RollupConfig struct {
	Enabled bool `mapstructure:"enabled"`
}

type ExporterConfig struct {
	Endpoint string `mapstructure:"endpoint"`
	Insecure bool   `mapstructure:"insecure"`
}

// DefaultConfig returns a configuration with default values
func DefaultConfig() *Config {
	return &Config{
		Debug: true, // Prototype default
		Receiver: ReceiverConfig{
			GRPCPort: 4317,
			HTTPPort: 4318,
			Host:     "0.0.0.0",
		},
		Buffer: BufferConfig{
			Size:       512, // 512MB
			TTLSeconds: 30,
			MaxTraces:  5000,
		},
		Discovery: DiscoveryConfig{
			Type: "static",
		},
		Gossip: GossipConfig{
			Port:     7946,
			BindAddr: "0.0.0.0",
			Interval: 100,
		},
		Decision: DecisionConfig{
			Policies: []PolicyConfig{
				{
					Name:               "sample-all",
					Type:               "latency",
					LatencyThresholdMS: -1,
				},
			},
		},
		Rollup: RollupConfig{
			Enabled: true,
		},
	}
}
