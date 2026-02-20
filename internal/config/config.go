package config

// Config holds the global configuration for the application
type Config struct {
	Debug     bool            `mapstructure:"debug" yaml:"debug"`
	Receiver  ReceiverConfig  `mapstructure:"receiver" yaml:"receiver"`
	Buffer    BufferConfig    `mapstructure:"buffer" yaml:"buffer"`
	Discovery DiscoveryConfig `mapstructure:"discovery" yaml:"discovery"`
	Gossip    GossipConfig    `mapstructure:"gossip" yaml:"gossip"`
	Decision  DecisionConfig  `mapstructure:"decision" yaml:"decision"`
	Rollup    RollupConfig    `mapstructure:"rollup" yaml:"rollup"`
	Exporter  ExporterConfig  `mapstructure:"exporter" yaml:"exporter"`
}

type ReceiverConfig struct {
	GRPCPort int    `mapstructure:"grpc_port" yaml:"grpc_port"`
	HTTPPort int    `mapstructure:"http_port" yaml:"http_port"`
	Host     string `mapstructure:"host" yaml:"host"`
}

type BufferConfig struct {
	Size        int `mapstructure:"size" yaml:"size"`
	TTLSeconds  int `mapstructure:"ttl_seconds" yaml:"ttl_seconds"`
	MaxTraces   int `mapstructure:"max_traces" yaml:"max_traces"`
}

type DiscoveryConfig struct {
	Type        string   `mapstructure:"type" yaml:"type"`
	ServiceName string   `mapstructure:"service_name" yaml:"service_name"`
	Peers       []string `mapstructure:"peers" yaml:"peers"`
}

type GossipConfig struct {
	Port         int    `mapstructure:"port" yaml:"port"`
	BindAddr     string `mapstructure:"bind_addr" yaml:"bind_addr"`
	Interval     int    `mapstructure:"interval" yaml:"interval"`
}

type DecisionConfig struct {
	Policies []PolicyConfig `mapstructure:"policies" yaml:"policies"`
}

type PolicyConfig struct {
	Name string `mapstructure:"name" yaml:"name"`
	Type string `mapstructure:"type" yaml:"type"`

	// Latency
	LatencyThresholdMS int64 `mapstructure:"latency_threshold_ms" yaml:"latency_threshold_ms"`

	// Attribute
	AttributeKey   string `mapstructure:"attribute_key" yaml:"attribute_key"`
	AttributeValue string `mapstructure:"attribute_value" yaml:"attribute_value"`

	// Probabilistic
	SampleRate float64 `mapstructure:"sample_rate" yaml:"sample_rate"`
}

type RollupConfig struct {
	Enabled bool `mapstructure:"enabled" yaml:"enabled"`
}

type ExporterConfig struct {
	Endpoint string `mapstructure:"endpoint" yaml:"endpoint"`
	Insecure bool   `mapstructure:"insecure" yaml:"insecure"`
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
