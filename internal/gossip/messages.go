package gossip

import (
	"time"
)

// MessageType defines the type of gossip message
type MessageType uint8

const (
	MsgSamplingDecision MessageType = iota
	MsgTriggerEvent
)

// BaseMessage is the common header
type BaseMessage struct {
	Type MessageType
}

// SamplingDecision is broadcast when a node decides a trace should be sampled
type SamplingDecision struct {
	TraceID    string    // Hex encoded TraceID
	Reason     string    // e.g., "latency", "error"
	DecidedAt  time.Time
	DecidedBy  string    // Node Name
	EventStart time.Time // Start time of the event/trace window
	EventEnd   time.Time // End time of the event/trace window
}

// TriggerEvent is broadcast when a global condition is met
type TriggerEvent struct {
	Name      string
	Value     float64
	Threshold float64
	StartedAt time.Time
}
