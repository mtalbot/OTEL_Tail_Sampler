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

// Resolver interface to allow mocking net.Resolver
type Resolver interface {
	LookupHost(ctx context.Context, host string) ([]string, error)
	LookupSRV(ctx context.Context, service, proto, name string) (string, []*net.SRV, error)
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

// DNSDiscovery looks up peers via DNS (SRV or A/AAAA records)
type DNSDiscovery struct {
	serviceName string
	resolver    Resolver
}

func NewDNSDiscovery(serviceName string) *DNSDiscovery {
	return &DNSDiscovery{
		serviceName: serviceName,
		resolver:    net.DefaultResolver,
	}
}

func (d *DNSDiscovery) GetPeers() ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// 1. Try SRV Lookup
	_, srvs, err := d.resolver.LookupSRV(ctx, "", "", d.serviceName)
	if err == nil && len(srvs) > 0 {
		var peers []string
		for _, srv := range srvs {
			// Resolve the target of the SRV record to an IP if possible, 
			// or just return hostname:port and let memberlist resolve it.
			// Ideally we return "target:port".
			// Note: SRV targets usually have a trailing dot, trim it.
			target := srv.Target
			if len(target) > 0 && target[len(target)-1] == '.' {
				target = target[:len(target)-1]
			}
			peers = append(peers, fmt.Sprintf("%s:%d", target, srv.Port))
		}
		sort.Strings(peers)
		return peers, nil
	}

	// 2. Fallback to Host Lookup (A/AAAA)
	addrs, err := d.resolver.LookupHost(ctx, d.serviceName)
	if err != nil {
		return nil, fmt.Errorf("failed to lookup %s: %w", d.serviceName, err)
	}

	sort.Strings(addrs)
	return addrs, nil
}