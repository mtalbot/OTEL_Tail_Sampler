package receiver

import (
	"context"
	"fmt"
	"net"
	"testing"
	"time"

	"OTEL_Tail_Sampler/internal/config"
	"OTEL_Tail_Sampler/pkg/signals"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/pdata/ptrace/ptraceotlp"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type MockConsumer struct {
	Received []signals.Signal
	Done     chan struct{}
}

func (m *MockConsumer) Consume(sig signals.Signal) error {
	m.Received = append(m.Received, sig)
	select {
	case m.Done <- struct{}{}:
	default:
	}
	return nil
}

func TestReceiver_Trace(t *testing.T) {
	// Find a free port
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()

	cfg := config.ReceiverConfig{
		Host:     "127.0.0.1",
		GRPCPort: port,
	}

	mockConsumer := &MockConsumer{
		Received: make([]signals.Signal, 0),
		Done:     make(chan struct{}, 1),
	}

	rcv := New(cfg, mockConsumer, true)
	err = rcv.Start(context.Background())
	require.NoError(t, err)
	defer rcv.Stop()

	// Wait for server to start
	time.Sleep(100 * time.Millisecond)

	// Create Client
	conn, err := grpc.NewClient(
		fmt.Sprintf("127.0.0.1:%d", port),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	require.NoError(t, err)
	defer conn.Close()

	client := ptraceotlp.NewGRPCClient(conn)

	// Send Trace
	req := ptraceotlp.NewExportRequest()
	traces := req.Traces()
	rss := traces.ResourceSpans().AppendEmpty()
	rss.Resource().Attributes().PutStr("service.name", "test-service")
	
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err = client.Export(ctx, req)
	require.NoError(t, err)

	// Wait for consumption
	select {
	case <-mockConsumer.Done:
	case <-time.After(2 * time.Second):
		t.Fatal("Timeout waiting for signal")
	}

	assert.Len(t, mockConsumer.Received, 1)
	sig, ok := mockConsumer.Received[0].(*signals.TraceSignal)
	assert.True(t, ok)
	assert.Equal(t, signals.TypeTrace, sig.Type())
	
	// Check content
	assert.Equal(t, 1, sig.Traces.ResourceSpans().Len())
	resArgs := sig.Traces.ResourceSpans().At(0).Resource().Attributes()
	val, ok := resArgs.Get("service.name")
	assert.True(t, ok)
	assert.Equal(t, "test-service", val.Str())
}