package gossip

import (
	"context"
	"fmt"
	"net"
	"os"
	"testing"
	"time"

	"OTEL_Tail_Sampler/internal/config"
	"OTEL_Tail_Sampler/internal/discovery"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// getFreePort returns a free TCP port
func getFreePort(t *testing.T) int {
	addr, err := net.ResolveTCPAddr("tcp", "localhost:0")
	require.NoError(t, err)

	l, err := net.ListenTCP("tcp", addr)
	require.NoError(t, err)
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

func TestGossipCluster(t *testing.T) {
	port1 := getFreePort(t)
	port2 := getFreePort(t)

	// Node 1 Config
	cfg1 := config.GossipConfig{
		Port:     port1,
		BindAddr: "127.0.0.1",
		Interval: 100,
	}
	// Node 1 جانتے initially (seed node)
	disc1, _ := discovery.New(config.DiscoveryConfig{Type: "static", Peers: []string{}})
	
	node1 := New(cfg1, disc1, true)
	err := node1.Start(context.Background())
	require.NoError(t, err)
	defer node1.Stop()

	// Node 2 Config
	cfg2 := config.GossipConfig{
		Port:     port2,
		BindAddr: "127.0.0.1",
		Interval: 100,
	}
	// Node 2 knows Node 1
	disc2, _ := discovery.New(config.DiscoveryConfig{
		Type:  "static", 
		Peers: []string{fmt.Sprintf("127.0.0.1:%d", port1)},
	})

	node2 := New(cfg2, disc2, true)
	err = node2.Start(context.Background())
	require.NoError(t, err)
	defer node2.Stop()

	// Wait for join
	require.Eventually(t, func() bool {
		return len(node1.Members()) == 2 && len(node2.Members()) == 2
	}, 5*time.Second, 100*time.Millisecond, "Cluster should form with 2 nodes")

	members := node1.Members()
	names := make(map[string]bool)
	for _, m := range members {
		names[m.Name] = true
	}
	assert.Contains(t, names, fmt.Sprintf("%s-%d", getHostname(), port1))
	assert.Contains(t, names, fmt.Sprintf("%s-%d", getHostname(), port2))
}

func getHostname() string {
	h, _ := os.Hostname()
	return h
}
