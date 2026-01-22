package gossip

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"OTEL_Tail_Sampler/internal/config"
	"OTEL_Tail_Sampler/internal/discovery"
	"OTEL_Tail_Sampler/internal/gossip/pb"

	"github.com/hashicorp/memberlist"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Manager handles the gossip protocol
type Manager struct {
	cfg        config.GossipConfig
	discoverer discovery.Discoverer
	list       *memberlist.Memberlist
	events     chan memberlist.NodeEvent
	debug      bool
	
	broadcasts   *memberlist.TransmitLimitedQueue
	decisionChan chan SamplingDecision
	triggerChan  chan TriggerEvent
}

// New creates a new Gossip Manager
func New(cfg config.GossipConfig, discoverer discovery.Discoverer, debug bool) *Manager {
	return &Manager{
		cfg:          cfg,
		discoverer:   discoverer,
		debug:        debug,
		events:       make(chan memberlist.NodeEvent, 16),
		decisionChan: make(chan SamplingDecision, 100),
		triggerChan:  make(chan TriggerEvent, 100),
	}
}

func (m *Manager) log(format string, v ...interface{}) {
	if m.debug {
		log.Printf("[GOSSIP] "+format, v...)
	}
}

// Start initializes memberlist and joins the cluster
func (m *Manager) Start(ctx context.Context) error {
	mlConfig := memberlist.DefaultLANConfig()
	mlConfig.BindPort = m.cfg.Port
	mlConfig.BindAddr = m.cfg.BindAddr
	
	hostname, _ := os.Hostname()
	mlConfig.Name = fmt.Sprintf("%s-%d", hostname, m.cfg.Port)

	// Delegates
	mlConfig.Events = &eventDelegate{events: m.events}
	mlConfig.Delegate = m

	list, err := memberlist.Create(mlConfig)
	if err != nil {
		return fmt.Errorf("failed to create memberlist: %w", err)
	}
	m.list = list
	m.log("Memberlist created on %s:%d", mlConfig.BindAddr, mlConfig.BindPort)

	m.broadcasts = &memberlist.TransmitLimitedQueue{
		NumNodes: func() int { return list.NumMembers() },
		RetransmitMult: 3,
	}

	log.Printf("Gossip started on %s:%d (Name: %s)", mlConfig.BindAddr, mlConfig.BindPort, mlConfig.Name)

	go m.joinLoop(ctx)

	return nil
}

func (m *Manager) Stop() {
	if m.list != nil {
		m.list.Leave(time.Second)
		m.list.Shutdown()
	}
}

func (m *Manager) GetDecisionChannel() <-chan SamplingDecision {
	return m.decisionChan
}

func (m *Manager) GetTriggerChannel() <-chan TriggerEvent {
	return m.triggerChan
}

func (m *Manager) BroadcastDecision(decision SamplingDecision) error {
	m.log("Broadcasting decision for trace %s", decision.TraceID)
	// Convert to PB
	pbMsg := &pb.GossipMessage{
		Payload: &pb.GossipMessage_SamplingDecision{
			SamplingDecision: &pb.SamplingDecision{
				TraceId:    decision.TraceID,
				Reason:     decision.Reason,
				DecidedAt:  timestamppb.New(decision.DecidedAt),
				DecidedBy:  decision.DecidedBy,
				EventStart: timestamppb.New(decision.EventStart),
				EventEnd:   timestamppb.New(decision.EventEnd),
			},
		},
	}

	// Marshal
	data, err := proto.Marshal(pbMsg)
	if err != nil {
		return err
	}

	m.broadcasts.QueueBroadcast(&broadcast{
		msg:    data,
		notify: nil,
	})

	// Also deliver locally
	select {
	case m.decisionChan <- decision:
	default:
		log.Printf("Decision channel full, dropping local message for %s", decision.TraceID)
	}

	return nil
}

func (m *Manager) BroadcastTrigger(trigger TriggerEvent) error {
	m.log("Broadcasting trigger event %s", trigger.Name)
	pbMsg := &pb.GossipMessage{
		Payload: &pb.GossipMessage_TriggerEvent{
			TriggerEvent: &pb.TriggerEvent{
				Name:      trigger.Name,
				Value:     trigger.Value,
				Threshold: trigger.Threshold,
				StartedAt: timestamppb.New(trigger.StartedAt),
			},
		},
	}

	data, err := proto.Marshal(pbMsg)
	if err != nil {
		return err
	}

	m.broadcasts.QueueBroadcast(&broadcast{
		msg:    data,
		notify: nil,
	})

	// Also deliver locally
	select {
	case m.triggerChan <- trigger:
	default:
		log.Printf("Trigger channel full, dropping local message for %s", trigger.Name)
	}

	return nil
}

// -- Delegate Interface --

func (m *Manager) NodeMeta(limit int) []byte {
	return []byte{}
}

func (m *Manager) NotifyMsg(b []byte) {
	if len(b) == 0 {
		return
	}
	m.log("Received gossip message, size %d", len(b))

	// Unmarshal
	pbMsg := &pb.GossipMessage{}
	if err := proto.Unmarshal(b, pbMsg); err != nil {
		log.Printf("Failed to unmarshal GossipMessage: %v", err)
		return
	}

	switch payload := pbMsg.Payload.(type) {
	case *pb.GossipMessage_SamplingDecision:
		sd := payload.SamplingDecision
		m.log("Decoded sampling decision for trace %s", sd.TraceId)
		decision := SamplingDecision{
			TraceID:    sd.TraceId,
			Reason:     sd.Reason,
			DecidedAt:  sd.DecidedAt.AsTime(),
			DecidedBy:  sd.DecidedBy,
			EventStart: sd.EventStart.AsTime(),
			EventEnd:   sd.EventEnd.AsTime(),
		}
		
		select {
		case m.decisionChan <- decision:
		default:
			log.Printf("Decision channel full, dropping message for %s", decision.TraceID)
		}
	
	case *pb.GossipMessage_TriggerEvent:
		te := payload.TriggerEvent
		trigger := TriggerEvent{
			Name:      te.Name,
			Value:     te.Value,
			Threshold: te.Threshold,
			StartedAt: te.StartedAt.AsTime(),
		}
		m.log("Received trigger event %s", trigger.Name)
		select {
		case m.triggerChan <- trigger:
		default:
			log.Printf("Trigger channel full, dropping message for %s", trigger.Name)
		}
	default:
		log.Printf("Unknown message payload type")
	}
}

func (m *Manager) GetBroadcasts(overhead, limit int) [][]byte {
	return m.broadcasts.GetBroadcasts(overhead, limit)
}

func (m *Manager) LocalState(join bool) []byte {
	return []byte{}
}

func (m *Manager) MergeRemoteState(buf []byte, join bool) {
}

// -- Internal --

func (m *Manager) joinLoop(ctx context.Context) {
	ticker := time.NewTicker(time.Duration(m.cfg.Interval) * time.Millisecond)
	if m.cfg.Interval == 0 {
		ticker.Reset(5 * time.Second)
	}
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if m.list.NumMembers() <= 1 {
				m.log("Attempting to join cluster...")
				m.tryJoin()
			}
		}
	}
}

func (m *Manager) tryJoin() {
	peers, err := m.discoverer.GetPeers()
	if err != nil {
		log.Printf("Failed to discover peers: %v", err)
		return
	}
	
	if len(peers) == 0 {
		return
	}
	
	n, err := m.list.Join(peers)
	if err != nil {
		m.log("Failed to join cluster: %v", err)
	} else if n > 0 {
		m.log("Joined %d peers", n)
	}
}

// Members returns the current list of cluster members
func (m *Manager) Members() []*memberlist.Node {
	if m.list == nil {
		return nil
	}
	return m.list.Members()
}

// broadcast implements memberlist.Broadcast
type broadcast struct {
	msg    []byte
	notify chan<- struct{}
}

func (b *broadcast) Invalidates(other memberlist.Broadcast) bool {
	return false
}

func (b *broadcast) Message() []byte {
	return b.msg
}

func (b *broadcast) Finished() {
	if b.notify != nil {
		close(b.notify)
	}
}

// eventDelegate captures node events
type eventDelegate struct {
	events chan memberlist.NodeEvent
}

func (e *eventDelegate) NotifyJoin(node *memberlist.Node) {
	log.Printf("Node joined: %s", node.Name)
}

func (e *eventDelegate) NotifyLeave(node *memberlist.Node) {
	log.Printf("Node left: %s", node.Name)
}

func (e *eventDelegate) NotifyUpdate(node *memberlist.Node) {
}
