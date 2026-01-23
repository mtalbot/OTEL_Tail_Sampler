package discovery

import (
	"context"
	"fmt"
	"net"
	"testing"

	"OTEL_Tail_Sampler/internal/config"

	"github.com/stretchr/testify/assert"
)

func TestStaticDiscovery(t *testing.T) {
	cfg := config.DiscoveryConfig{
		Type:  "static",
		Peers: []string{"10.0.0.1", "10.0.0.2"},
	}
	d, err := New(cfg)
	assert.NoError(t, err)
	assert.NotNil(t, d)

	peers, err := d.GetPeers()
	assert.NoError(t, err)
	assert.Equal(t, []string{"10.0.0.1", "10.0.0.2"}, peers)
}

type MockResolver struct {
	srvs  []*net.SRV
	hosts []string
	err   error
}

func (m *MockResolver) LookupSRV(ctx context.Context, service, proto, name string) (string, []*net.SRV, error) {
	if len(m.srvs) > 0 {
		return "", m.srvs, nil
	}
	return "", nil, fmt.Errorf("no srv records")
}

func (m *MockResolver) LookupHost(ctx context.Context, host string) ([]string, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.hosts, nil
}

func TestDNSDiscovery_SRV(t *testing.T) {
	mock := &MockResolver{
		srvs: []*net.SRV{
			{Target: "node1.local.", Port: 8080},
			{Target: "node2.local.", Port: 8080},
		},
	}

	d := NewDNSDiscovery("myservice")
	d.resolver = mock

	peers, err := d.GetPeers()
	assert.NoError(t, err)
	assert.Equal(t, []string{"node1.local:8080", "node2.local:8080"}, peers)
}

func TestDNSDiscovery_HostFallback(t *testing.T) {
	mock := &MockResolver{
		hosts: []string{"192.168.1.1", "192.168.1.2"},
	}

	d := NewDNSDiscovery("myservice")
	d.resolver = mock

	peers, err := d.GetPeers()
	assert.NoError(t, err)
	assert.Equal(t, []string{"192.168.1.1", "192.168.1.2"}, peers)
}

func TestUnknownDiscovery(t *testing.T) {
	cfg := config.DiscoveryConfig{Type: "invalid"}
	_, err := New(cfg)
	assert.Error(t, err)
}