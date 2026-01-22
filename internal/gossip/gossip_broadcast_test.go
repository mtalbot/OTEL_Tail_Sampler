package gossip

import (
	"context"
	"fmt"
	"testing"
	"time"

	"OTEL_Tail_Sampler/internal/config"
	"OTEL_Tail_Sampler/internal/discovery"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGossipBroadcast(t *testing.T) {
	port1 := getFreePort(t)
	port2 := getFreePort(t)

	// Node 1
	cfg1 := config.GossipConfig{Port: port1, BindAddr: "127.0.0.1", Interval: 100}
	disc1, _ := discovery.New(config.DiscoveryConfig{Type: "static", Peers: []string{}})
	node1 := New(cfg1, disc1, true)
	node1.Start(context.Background())
	defer node1.Stop()

	// Node 2
	cfg2 := config.GossipConfig{Port: port2, BindAddr: "127.0.0.1", Interval: 100}
	disc2, _ := discovery.New(config.DiscoveryConfig{Type: "static", Peers: []string{fmt.Sprintf("127.0.0.1:%d", port1)}})
	node2 := New(cfg2, disc2, true)
	node2.Start(context.Background())
	defer node2.Stop()

	// Wait for Cluster
	require.Eventually(t, func() bool {
		return len(node1.Members()) == 2 && len(node2.Members()) == 2
	}, 5*time.Second, 100*time.Millisecond)

	// Broadcast from Node 1
	decision := SamplingDecision{
		TraceID:    "abcdef123456",
		Reason:     "high_latency",
		DecidedAt:  time.Now(),
		DecidedBy:  "node1",
		EventStart: time.Now().Add(-10 * time.Second),
		EventEnd:   time.Now(),
	}
	
	err := node1.BroadcastDecision(decision)
	require.NoError(t, err)

	// Receive on Node 2
	select {
	case received := <-node2.GetDecisionChannel():
		assert.Equal(t, decision.TraceID, received.TraceID)
		assert.Equal(t, decision.Reason, received.Reason)
		assert.Equal(t, decision.DecidedBy, received.DecidedBy)
		// MsgPack time precision might differ slightly (serialization usually strips monotonic clock)
		assert.True(t, decision.EventStart.Equal(received.EventStart), "EventStart mismatch")
		assert.True(t, decision.EventEnd.Equal(received.EventEnd), "EventEnd mismatch")
	case <-time.After(2 * time.Second):
		t.Fatal("Timeout waiting for broadcast message")
	}
}
