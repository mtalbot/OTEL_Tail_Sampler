package discovery

import (
	"context"
	"fmt"
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

func TestDNSDiscovery(t *testing.T) {
	// Mock Lookup
	mockLookup := func(ctx context.Context, host string) ([]string, error) {
		if host == "myservice" {
			return []string{"192.168.1.2", "192.168.1.1"}, nil
		}
		return nil, fmt.Errorf("lookup failed")
	}

	d := NewDNSDiscovery("myservice")
	d.lookupHost = mockLookup // Inject mock

	peers, err := d.GetPeers()
	assert.NoError(t, err)
	// Output should be sorted
	assert.Equal(t, []string{"192.168.1.1", "192.168.1.2"}, peers)
}

func TestUnknownDiscovery(t *testing.T) {
	cfg := config.DiscoveryConfig{Type: "invalid"}
	_, err := New(cfg)
	assert.Error(t, err)
}
