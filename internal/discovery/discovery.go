package discovery

import (
	"context"
	"fmt"
	"net"
	"sort"
	"time"

	"OTEL_Tail_Sampler/internal/config"
)

// Discoverer finds peer nodes in the cluster
type Discoverer interface {
	GetPeers() ([]string, error)
}

// New creates a new Discoverer based on config
func New(cfg config.DiscoveryConfig) (Discoverer, error) {
	switch cfg.Type {
	case "static":
		return &StaticDiscovery{peers: cfg.Peers}, nil
	case "dns":
		return NewDNSDiscovery(cfg.ServiceName), nil
	default:
		return nil, fmt.Errorf("unknown discovery type: %s", cfg.Type)
	}
}

// StaticDiscovery returns a fixed list of peers
type StaticDiscovery struct {
	peers []string
}

func (s *StaticDiscovery) GetPeers() ([]string, error) {
	return s.peers, nil
}

// DNSDiscovery looks up peers via DNS (A/AAAA records)
type DNSDiscovery struct {
	serviceName string
	lookupHost  func(ctx context.Context, host string) ([]string, error)
}

func NewDNSDiscovery(serviceName string) *DNSDiscovery {
	return &DNSDiscovery{
		serviceName: serviceName,
		lookupHost:  net.DefaultResolver.LookupHost,
	}
}

func (d *DNSDiscovery) GetPeers() ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// LookupHost returns a slice of the host's IPv4 and IPv6 addresses
	addrs, err := d.lookupHost(ctx, d.serviceName)
	if err != nil {
		return nil, fmt.Errorf("failed to lookup %s: %w", d.serviceName, err)
	}

	sort.Strings(addrs)
	return addrs, nil
}
