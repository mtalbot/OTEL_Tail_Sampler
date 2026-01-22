package buffer

import (
	"bytes"
	"context"
	"encoding/gob"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"OTEL_Tail_Sampler/internal/config"
	"OTEL_Tail_Sampler/pkg/signals"

	"github.com/allegro/bigcache/v3"
	"github.com/google/btree"
	"github.com/google/uuid"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/plog"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.opentelemetry.io/collector/pdata/ptrace"
)

var (
	ErrBufferFull = errors.New("buffer is full")
)

// Manager manages the buffering of signals
type Manager interface {
	Start(ctx context.Context) error
	Stop()
	Store(sig signals.Signal) error
	
	// Trace methods
	GetTrace(traceID pcommon.TraceID) (*signals.TraceSignal, bool)
	GetTraceLogs(traceID pcommon.TraceID) (*signals.LogSignal, bool)
	RemoveTrace(traceID pcommon.TraceID)
	GetTraceCount() int

	// Metric & Log methods
	GetMetricsInRange(start, end time.Time) []*signals.MetricSignal
	GetLogsInRange(start, end time.Time) []*signals.LogSignal
	RemoveMetricsInRange(start, end time.Time)
	RemoveLogsInRange(start, end time.Time)
	
	ClearMetrics()
	ClearLogs()
}

// StoredTraceData is what we serialize to BigCache
type StoredTraceData struct {
	ReceivedAt int64  // UnixNano
	TracesData []byte // Serialized ptrace.Traces
	LogsData   []byte // Serialized plog.Logs
}

// TimeKey implements btree.Item for time-ordered indexing
type TimeKey struct {
	Timestamp int64
	Key       string
}

func (t *TimeKey) Less(than btree.Item) bool {
	other := than.(*TimeKey)
	if t.Timestamp != other.Timestamp {
		return t.Timestamp < other.Timestamp
	}
	return t.Key < other.Key
}

// InMemoryBuffer implements Manager using BigCache and B-Tree indices
type InMemoryBuffer struct {
	cfg   config.BufferConfig
	cache *bigcache.BigCache
	mu    sync.RWMutex
	debug bool

	// Indices for time-range lookups (O(log N))
	metricTimeIndex *btree.BTree
	logTimeIndex    *btree.BTree

	// Lookup by Key for efficient removal (O(1))
	metricKeyMap map[string]int64
	logKeyMap    map[string]int64
	
	// Marshalers
	traceMarshaler    ptrace.ProtoMarshaler
	traceUnmarshaler  ptrace.ProtoUnmarshaler
	logMarshaler      plog.ProtoMarshaler
	logUnmarshaler    plog.ProtoUnmarshaler
	metricMarshaler   pmetric.ProtoMarshaler
	metricUnmarshaler pmetric.ProtoUnmarshaler
	
	stopChan chan struct{}
}

func New(cfg config.BufferConfig, debug bool) *InMemoryBuffer {
	b := &InMemoryBuffer{
		cfg:             cfg,
		debug:           debug,
		metricTimeIndex: btree.New(32),
		logTimeIndex:    btree.New(32),
		metricKeyMap:    make(map[string]int64),
		logKeyMap:       make(map[string]int64),
		stopChan:        make(chan struct{}),
	}

	// Initialize BigCache with Eviction Hook
	cacheConfig := bigcache.DefaultConfig(time.Duration(cfg.TTLSeconds) * time.Second)
	cacheConfig.CleanWindow = 1 * time.Second
	cacheConfig.HardMaxCacheSize = cfg.Size
	if cfg.Size == 0 {
		cacheConfig.HardMaxCacheSize = 512
	}

	// Automatic Index Cleanup Hook
	cacheConfig.OnRemove = b.onCacheEviction

	cache, err := bigcache.New(context.Background(), cacheConfig)
	if err != nil {
		panic(fmt.Sprintf("failed to init bigcache: %v", err))
	}
	b.cache = cache

	return b
}

func (b *InMemoryBuffer) log(format string, v ...interface{}) {
	if b.debug {
		log.Printf("[BUFFER] "+format, v...)
	}
}

// onCacheEviction is called by BigCache when an item is removed (expired, no space, or deleted)
func (b *InMemoryBuffer) onCacheEviction(key string, entry []byte) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if strings.HasPrefix(key, "m_") {
		if ts, ok := b.metricKeyMap[key]; ok {
			b.metricTimeIndex.Delete(&TimeKey{Timestamp: ts, Key: key})
			delete(b.metricKeyMap, key)
			b.log("Evicted metric index for key %s", key)
		}
	} else if strings.HasPrefix(key, "l_") {
		if ts, ok := b.logKeyMap[key]; ok {
			b.logTimeIndex.Delete(&TimeKey{Timestamp: ts, Key: key})
			delete(b.logKeyMap, key)
			b.log("Evicted log index for key %s", key)
		}
	}
}

func (b *InMemoryBuffer) Start(ctx context.Context) error {
	return nil
}

func (b *InMemoryBuffer) Stop() {
	if b.cache != nil {
		b.cache.Close()
	}
}

func (b *InMemoryBuffer) Store(sig signals.Signal) error {
	// Storage methods handle their own locking for index updates
	switch s := sig.(type) {
	case *signals.TraceSignal:
		return b.storeTraces(s)
	case *signals.MetricSignal:
		return b.storeMetric(s)
	case *signals.LogSignal:
		return b.storeSplitLogs(s)
	default:
		return nil
	}
}

func (b *InMemoryBuffer) storeTraces(sig *signals.TraceSignal) error {
	rss := sig.Traces.ResourceSpans()
	traceMap := make(map[pcommon.TraceID]ptrace.Traces)
	b.log("Processing trace signal batch")

	for i := 0; i < rss.Len(); i++ {
		rs := rss.At(i)
		ilss := rs.ScopeSpans()
		for j := 0; j < ilss.Len(); j++ {
			ils := ilss.At(j)
			spans := ils.Spans()
			for k := 0; k < spans.Len(); k++ {
				span := spans.At(k)
				tid := span.TraceID()
				if tid.IsEmpty() {
					continue
				}
				if _, ok := traceMap[tid]; !ok {
					traceMap[tid] = ptrace.NewTraces()
				}
				t := traceMap[tid]
				newRS := t.ResourceSpans().AppendEmpty()
				rs.Resource().CopyTo(newRS.Resource())
				newILS := newRS.ScopeSpans().AppendEmpty()
				ils.Scope().CopyTo(newILS.Scope())
				newSpan := newILS.Spans().AppendEmpty()
				span.CopyTo(newSpan)
			}
		}
	}

	for tid, newTraces := range traceMap {
		key := hex.EncodeToString(tid[:])
		var stored StoredTraceData
		entry, err := b.cache.Get(key)
		if err == nil {
			dec := gob.NewDecoder(bytes.NewReader(entry))
			dec.Decode(&stored)
		} else {
			stored = StoredTraceData{ReceivedAt: time.Now().UnixNano()}
		}

		var currentTraces ptrace.Traces
		if len(stored.TracesData) > 0 {
			currentTraces, _ = b.traceUnmarshaler.UnmarshalTraces(stored.TracesData)
		} else {
			currentTraces = ptrace.NewTraces()
		}
		newTraces.ResourceSpans().MoveAndAppendTo(currentTraces.ResourceSpans())
		data, _ := b.traceMarshaler.MarshalTraces(currentTraces)
		stored.TracesData = data

		var buf bytes.Buffer
		gob.NewEncoder(&buf).Encode(stored)
		b.cache.Set(key, buf.Bytes())
	}
	return nil
}

func (b *InMemoryBuffer) storeSplitLogs(sig *signals.LogSignal) error {
	b.log("Processing log signal batch")
	traceLogs := make(map[pcommon.TraceID]plog.Logs)
	var timeLogs plog.Logs
	timeLogsInit := false

	rls := sig.Logs.ResourceLogs()
	for i := 0; i < rls.Len(); i++ {
		rl := rls.At(i)
		sls := rl.ScopeLogs()
		for j := 0; j < sls.Len(); j++ {
			sl := sls.At(j)
			logs := sl.LogRecords()
			for k := 0; k < logs.Len(); k++ {
				lr := logs.At(k)
				tid := lr.TraceID()
				if !tid.IsEmpty() {
					if _, ok := traceLogs[tid]; !ok {
						traceLogs[tid] = plog.NewLogs()
					}
					l := traceLogs[tid]
					newRL := l.ResourceLogs().AppendEmpty()
					rl.Resource().CopyTo(newRL.Resource())
					newSL := newRL.ScopeLogs().AppendEmpty()
					sl.Scope().CopyTo(newSL.Scope())
					newLR := newSL.LogRecords().AppendEmpty()
					lr.CopyTo(newLR)
				} else {
					if !timeLogsInit {
						timeLogs = plog.NewLogs()
						timeLogsInit = true
					}
					newRL := timeLogs.ResourceLogs().AppendEmpty()
					rl.Resource().CopyTo(newRL.Resource())
					newSL := newRL.ScopeLogs().AppendEmpty()
					sl.Scope().CopyTo(newSL.Scope())
					newLR := newSL.LogRecords().AppendEmpty()
					lr.CopyTo(newLR)
				}
			}
		}
	}

	for tid, logs := range traceLogs {
		key := hex.EncodeToString(tid[:])
		var stored StoredTraceData
		entry, err := b.cache.Get(key)
		if err == nil {
			gob.NewDecoder(bytes.NewReader(entry)).Decode(&stored)
		} else {
			stored = StoredTraceData{ReceivedAt: time.Now().UnixNano()}
		}
		var currentLogs plog.Logs
		if len(stored.LogsData) > 0 {
			currentLogs, _ = b.logUnmarshaler.UnmarshalLogs(stored.LogsData)
		} else {
			currentLogs = plog.NewLogs()
		}
		logs.ResourceLogs().MoveAndAppendTo(currentLogs.ResourceLogs())
		data, _ := b.logMarshaler.MarshalLogs(currentLogs)
		stored.LogsData = data
		var buf bytes.Buffer
		gob.NewEncoder(&buf).Encode(stored)
		b.cache.Set(key, buf.Bytes())
	}

	if timeLogsInit {
		data, err := b.logMarshaler.MarshalLogs(timeLogs)
		if err == nil {
			ts := time.Now().UnixNano()
			key := fmt.Sprintf("l_%d_%s", ts, uuid.NewString())
			if err := b.cache.Set(key, data); err == nil {
				b.mu.Lock()
				b.logKeyMap[key] = ts
				b.logTimeIndex.ReplaceOrInsert(&TimeKey{Timestamp: ts, Key: key})
				b.mu.Unlock()
				b.log("Indexed time-bound log %s", key)
			}
		}
	}
	return nil
}

func (b *InMemoryBuffer) storeMetric(sig *signals.MetricSignal) error {
	b.log("Processing metric signal")
	data, err := b.metricMarshaler.MarshalMetrics(sig.Metrics)
	if err != nil {
		return err
	}
	ts := time.Now().UnixNano()
	key := fmt.Sprintf("m_%d_%s", ts, uuid.NewString())
	if err := b.cache.Set(key, data); err == nil {
		b.mu.Lock()
		b.metricKeyMap[key] = ts
		b.metricTimeIndex.ReplaceOrInsert(&TimeKey{Timestamp: ts, Key: key})
		b.mu.Unlock()
		b.log("Indexed metric %s", key)
	}
	return nil
}

func (b *InMemoryBuffer) GetTrace(traceID pcommon.TraceID) (*signals.TraceSignal, bool) {
	key := hex.EncodeToString(traceID[:])
	entry, err := b.cache.Get(key)
	if err != nil {
		return nil, false
	}
	var stored StoredTraceData
	if err := gob.NewDecoder(bytes.NewReader(entry)).Decode(&stored); err != nil || len(stored.TracesData) == 0 {
		return nil, false
	}
	traces, _ := b.traceUnmarshaler.UnmarshalTraces(stored.TracesData)
	return &signals.TraceSignal{
		Traces: traces,
		Ctx:    signals.Context{ReceivedAt: time.Unix(0, stored.ReceivedAt)},
	}, true
}

func (b *InMemoryBuffer) GetTraceLogs(traceID pcommon.TraceID) (*signals.LogSignal, bool) {
	key := hex.EncodeToString(traceID[:])
	entry, err := b.cache.Get(key)
	if err != nil {
		return nil, false
	}
	var stored StoredTraceData
	if err := gob.NewDecoder(bytes.NewReader(entry)).Decode(&stored); err != nil || len(stored.LogsData) == 0 {
		return nil, false
	}
	logs, _ := b.logUnmarshaler.UnmarshalLogs(stored.LogsData)
	return &signals.LogSignal{
		Logs: logs,
		Ctx:    signals.Context{ReceivedAt: time.Unix(0, stored.ReceivedAt)},
	}, true
}

func (b *InMemoryBuffer) GetTraceCount() int {
	return b.cache.Len()
}

func (b *InMemoryBuffer) RemoveTrace(traceID pcommon.TraceID) {
	key := hex.EncodeToString(traceID[:])
	b.cache.Delete(key) // This triggers onCacheEviction hook
}

func (b *InMemoryBuffer) GetMetricsInRange(start, end time.Time) []*signals.MetricSignal {
	b.mu.RLock()
	defer b.mu.RUnlock()
	
	var result []*signals.MetricSignal
	b.metricTimeIndex.AscendRange(&TimeKey{Timestamp: start.UnixNano()}, &TimeKey{Timestamp: end.UnixNano() + 1}, func(item btree.Item) bool {
		tk := item.(*TimeKey)
		data, err := b.cache.Get(tk.Key)
		if err == nil {
			m, err := b.metricUnmarshaler.UnmarshalMetrics(data)
			if err == nil {
				result = append(result, &signals.MetricSignal{
					Metrics: m,
					Ctx: signals.Context{ReceivedAt: time.Unix(0, tk.Timestamp)},
				})
			}
		}
		return true
	})
	return result
}

func (b *InMemoryBuffer) GetLogsInRange(start, end time.Time) []*signals.LogSignal {
	b.mu.RLock()
	defer b.mu.RUnlock()
	
	var result []*signals.LogSignal
	b.logTimeIndex.AscendRange(&TimeKey{Timestamp: start.UnixNano()}, &TimeKey{Timestamp: end.UnixNano() + 1}, func(item btree.Item) bool {
		tk := item.(*TimeKey)
		data, err := b.cache.Get(tk.Key)
		if err == nil {
			l, err := b.logUnmarshaler.UnmarshalLogs(data)
			if err == nil {
				result = append(result, &signals.LogSignal{
					Logs: l,
					Ctx: signals.Context{ReceivedAt: time.Unix(0, tk.Timestamp)},
				})
			}
		}
		return true
	})
	return result
}

func (b *InMemoryBuffer) RemoveMetricsInRange(start, end time.Time) {
	b.mu.RLock()
	var keys []string
	b.metricTimeIndex.AscendRange(&TimeKey{Timestamp: start.UnixNano()}, &TimeKey{Timestamp: end.UnixNano() + 1}, func(item btree.Item) bool {
		keys = append(keys, item.(*TimeKey).Key)
		return true
	})
	b.mu.RUnlock()

	for _, k := range keys {
		b.cache.Delete(k)
	}
}

func (b *InMemoryBuffer) RemoveLogsInRange(start, end time.Time) {
	b.mu.RLock()
	var keys []string
	b.logTimeIndex.AscendRange(&TimeKey{Timestamp: start.UnixNano()}, &TimeKey{Timestamp: end.UnixNano() + 1}, func(item btree.Item) bool {
		keys = append(keys, item.(*TimeKey).Key)
		return true
	})
	b.mu.RUnlock()

	for _, k := range keys {
		b.cache.Delete(k)
	}
}

func (b *InMemoryBuffer) ClearMetrics() {
	b.mu.Lock()
	var keys []string
	for k := range b.metricKeyMap {
		keys = append(keys, k)
	}
	b.mu.Unlock()
	for _, k := range keys {
		b.cache.Delete(k)
	}
}

func (b *InMemoryBuffer) ClearLogs() {
	b.mu.Lock()
	var keys []string
	for k := range b.logKeyMap {
		keys = append(keys, k)
	}
	b.mu.Unlock()
	for _, k := range keys {
		b.cache.Delete(k)
	}
}
