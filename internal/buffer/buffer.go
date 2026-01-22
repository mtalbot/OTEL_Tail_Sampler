package buffer

import (
	"bytes"
	"context"
	"encoding/gob"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"OTEL_Tail_Sampler/internal/config"
	"OTEL_Tail_Sampler/pkg/signals"

	"github.com/allegro/bigcache/v3"
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

type TimeKey struct {
	Timestamp int64
	Key       string
}

// InMemoryBuffer implements Manager using BigCache
type InMemoryBuffer struct {
	cfg   config.BufferConfig
	cache *bigcache.BigCache
	mu    sync.RWMutex
	debug bool

	// Indices for time-range lookups
	metricIndex []TimeKey
	logIndex    []TimeKey
	
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
	// Initialize BigCache
	cacheConfig := bigcache.DefaultConfig(time.Duration(cfg.TTLSeconds) * time.Second)
	cacheConfig.CleanWindow = 1 * time.Second
	// Allow large entries if needed, though default is usually fine
	cacheConfig.HardMaxCacheSize = cfg.Size // in MB? config says "Size int", usually MB in bigcache configs
	if cfg.Size == 0 {
		cacheConfig.HardMaxCacheSize = 512 // Default 512MB
	}

	cache, err := bigcache.New(context.Background(), cacheConfig)
	if err != nil {
		// Fallback or panic? In this context, panic is likely appropriate as it's init
		panic(fmt.Sprintf("failed to init bigcache: %v", err))
	}

	return &InMemoryBuffer{
		cfg:         cfg,
		cache:       cache,
		debug:       debug,
		metricIndex: make([]TimeKey, 0),
		logIndex:    make([]TimeKey, 0),
		stopChan:    make(chan struct{}),
	}
}

func (b *InMemoryBuffer) log(format string, v ...interface{}) {
	if b.debug {
		log.Printf("[BUFFER] "+format, v...)
	}
}

func (b *InMemoryBuffer) Start(ctx context.Context) error {
	// No background loop as requested
	return nil
}

func (b *InMemoryBuffer) Stop() {
	if b.cache != nil {
		b.cache.Close()
	}
}

func (b *InMemoryBuffer) Store(sig signals.Signal) error {
	b.mu.Lock()
	defer b.mu.Unlock()

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
	// Group spans by TraceID
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
				
				// Create new Traces for this ID if needed
				if _, ok := traceMap[tid]; !ok {
					traceMap[tid] = ptrace.NewTraces()
				}
				t := traceMap[tid]
				
				// Copy structure
				// Simplified: Create new RS/ILS/Span
				newRS := t.ResourceSpans().AppendEmpty()
				rs.Resource().CopyTo(newRS.Resource())
				newILS := newRS.ScopeSpans().AppendEmpty()
				ils.Scope().CopyTo(newILS.Scope())
				newSpan := newILS.Spans().AppendEmpty()
				span.CopyTo(newSpan)
			}
		}
	}

	// Update BigCache for each TraceID
	for tid, newTraces := range traceMap {
		key := hex.EncodeToString(tid[:])
		
		var stored StoredTraceData
		
		// Check existing
		entry, err := b.cache.Get(key)
		if err == nil {
			// Deserialize existing
			dec := gob.NewDecoder(bytes.NewReader(entry))
			if err := dec.Decode(&stored); err != nil {
				// Corrupt? Overwrite.
				stored = StoredTraceData{ReceivedAt: time.Now().UnixNano()}
			}
		} else {
			stored = StoredTraceData{ReceivedAt: time.Now().UnixNano()}
		}

		// Merge Traces
		var currentTraces ptrace.Traces
		if len(stored.TracesData) > 0 {
			currentTraces, err = b.traceUnmarshaler.UnmarshalTraces(stored.TracesData)
			if err != nil {
				// Log error? Start new
				currentTraces = ptrace.NewTraces()
			}
		} else {
			currentTraces = ptrace.NewTraces()
		}
		
		// Move new spans to currentTraces
		// ptrace doesn't have a simple "Merge", we iterate and append
		newTraces.ResourceSpans().MoveAndAppendTo(currentTraces.ResourceSpans())
		
		// Reserialize
		data, err := b.traceMarshaler.MarshalTraces(currentTraces)
		if err != nil {
			continue
		}
		stored.TracesData = data

		// Encode and Store
		var buf bytes.Buffer
		enc := gob.NewEncoder(&buf)
		if err := enc.Encode(stored); err != nil {
			continue
		}
		
		if err := b.cache.Set(key, buf.Bytes()); err != nil {
			b.log("Failed to set trace in cache: %v", err)
		} else {
			b.log("Stored trace %s in cache", key)
		}
	}
	return nil
}

func (b *InMemoryBuffer) storeSplitLogs(sig *signals.LogSignal) error {
	b.log("Processing log signal batch")
	// Split into Trace-bound and Time-bound
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
				logRecord := logs.At(k)
				traceID := logRecord.TraceID()
				
				if !traceID.IsEmpty() {
					// Add to traceLogs
					if _, ok := traceLogs[traceID]; !ok {
						traceLogs[traceID] = plog.NewLogs()
					}
					l := traceLogs[traceID]
					newRL := l.ResourceLogs().AppendEmpty()
					rl.Resource().CopyTo(newRL.Resource())
					newSL := newRL.ScopeLogs().AppendEmpty()
					sl.Scope().CopyTo(newSL.Scope())
					newLR := newSL.LogRecords().AppendEmpty()
					logRecord.CopyTo(newLR)
				} else {
					// Add to timeLogs
					if !timeLogsInit {
						timeLogs = plog.NewLogs()
						timeLogsInit = true
					}
					newRL := timeLogs.ResourceLogs().AppendEmpty()
					rl.Resource().CopyTo(newRL.Resource())
					newSL := newRL.ScopeLogs().AppendEmpty()
					sl.Scope().CopyTo(newSL.Scope())
					newLR := newSL.LogRecords().AppendEmpty()
					logRecord.CopyTo(newLR)
				}
			}
		}
	}

	// 1. Store Trace-bound logs in BigCache (update TraceData)
	for tid, logs := range traceLogs {
		key := hex.EncodeToString(tid[:])
		var stored StoredTraceData
		
		entry, err := b.cache.Get(key)
		if err == nil {
			dec := gob.NewDecoder(bytes.NewReader(entry))
			if err := dec.Decode(&stored); err != nil {
				stored = StoredTraceData{ReceivedAt: time.Now().UnixNano()}
			}
		} else {
			stored = StoredTraceData{ReceivedAt: time.Now().UnixNano()}
		}

		var currentLogs plog.Logs
		if len(stored.LogsData) > 0 {
			var err error
			currentLogs, err = b.logUnmarshaler.UnmarshalLogs(stored.LogsData)
			if err != nil {
				currentLogs = plog.NewLogs()
			}
		} else {
			currentLogs = plog.NewLogs()
		}
		
		logs.ResourceLogs().MoveAndAppendTo(currentLogs.ResourceLogs())
		
		data, _ := b.logMarshaler.MarshalLogs(currentLogs)
		stored.LogsData = data
		
		var buf bytes.Buffer
		enc := gob.NewEncoder(&buf)
		enc.Encode(stored)
		b.cache.Set(key, buf.Bytes())
		b.log("Updated trace-bound logs for %s", key)
	}

	// 2. Store Time-bound logs as separate entries
	if timeLogsInit {
		// Serialize
		data, err := b.logMarshaler.MarshalLogs(timeLogs)
		if err == nil {
			// Generate Key: l_<timestamp>_<uuid>
			ts := time.Now().UnixNano()
			key := fmt.Sprintf("l_%d_%s", ts, uuid.NewString())
			
			if err := b.cache.Set(key, data); err == nil {
				// Update Index
				b.logIndex = append(b.logIndex, TimeKey{Timestamp: ts, Key: key})
				b.log("Stored time-bound log with key %s", key)
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
		b.metricIndex = append(b.metricIndex, TimeKey{Timestamp: ts, Key: key})
		b.log("Stored metric with key %s", key)
	}
	return nil
}

// Trace Retrieval
func (b *InMemoryBuffer) GetTrace(traceID pcommon.TraceID) (*signals.TraceSignal, bool) {
	key := hex.EncodeToString(traceID[:])
	entry, err := b.cache.Get(key)
	if err != nil {
		b.log("Trace %s not found in cache", key)
		return nil, false
	}
	
	var stored StoredTraceData
	dec := gob.NewDecoder(bytes.NewReader(entry))
	if err := dec.Decode(&stored); err != nil {
		return nil, false
	}
	
	if len(stored.TracesData) == 0 {
		return nil, false
	}
	
	traces, err := b.traceUnmarshaler.UnmarshalTraces(stored.TracesData)
	if err != nil {
		return nil, false
	}
	
	b.log("Retrieved trace %s from cache", key)
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
	dec := gob.NewDecoder(bytes.NewReader(entry))
	if err := dec.Decode(&stored); err != nil {
		return nil, false
	}
	
	if len(stored.LogsData) == 0 {
		return nil, false
	}
	
	logs, err := b.logUnmarshaler.UnmarshalLogs(stored.LogsData)
	if err != nil {
		return nil, false
	}
	
	return &signals.LogSignal{
		Logs: logs,
		Ctx:    signals.Context{ReceivedAt: time.Unix(0, stored.ReceivedAt)},
	}, true
}

func (b *InMemoryBuffer) RemoveTrace(traceID pcommon.TraceID) {
	key := hex.EncodeToString(traceID[:])
	b.log("Removing trace %s from cache", key)
	b.cache.Delete(key)
}

// Range Retrieval
func (b *InMemoryBuffer) GetMetricsInRange(start, end time.Time) []*signals.MetricSignal {
	b.mu.Lock() // Changed to Lock for index modification
	defer b.mu.Unlock()
	
	b.log("Fetching metrics in range %v to %v", start, end)

	// Lazy cleanup: Check head
	b.cleanHead(&b.metricIndex)

	s := start.UnixNano()
	e := end.UnixNano()
	
	var result []*signals.MetricSignal
	newIndex := make([]TimeKey, 0, len(b.metricIndex))
	
	for _, tk := range b.metricIndex {
		// Pruning check: if miss, don't keep in index
		data, err := b.cache.Get(tk.Key)
		if err != nil {
			// Miss (Expired), skip (effectively removes from index)
			continue
		}
		
		newIndex = append(newIndex, tk)

		// Range check
		if tk.Timestamp >= s && tk.Timestamp <= e {
			m, err := b.metricUnmarshaler.UnmarshalMetrics(data)
			if err == nil {
				result = append(result, &signals.MetricSignal{
					Metrics: m,
					Ctx: signals.Context{ReceivedAt: time.Unix(0, tk.Timestamp)},
				})
			}
		}
	}
	// Update index with live keys
	b.metricIndex = newIndex
	
	b.log("Found %d metrics in range", len(result))
	return result
}

func (b *InMemoryBuffer) GetLogsInRange(start, end time.Time) []*signals.LogSignal {
	b.mu.Lock() // Changed to Lock
	defer b.mu.Unlock()
	
	b.log("Fetching logs in range %v to %v", start, end)

	// Lazy cleanup: Check head
	b.cleanHead(&b.logIndex)

	s := start.UnixNano()
	e := end.UnixNano()
	
	var result []*signals.LogSignal
	newIndex := make([]TimeKey, 0, len(b.logIndex))
	
	for _, tk := range b.logIndex {
		data, err := b.cache.Get(tk.Key)
		if err != nil {
			// Miss (Expired)
			continue
		}
		
		newIndex = append(newIndex, tk)

		if tk.Timestamp >= s && tk.Timestamp <= e {
			l, err := b.logUnmarshaler.UnmarshalLogs(data)
			if err == nil {
				result = append(result, &signals.LogSignal{
					Logs: l,
					Ctx: signals.Context{ReceivedAt: time.Unix(0, tk.Timestamp)},
				})
			}
		}
	}
	b.logIndex = newIndex

	b.log("Found %d logs in range", len(result))
	return result
}

func (b *InMemoryBuffer) cleanHead(index *[]TimeKey) {
	// Prune dead keys from the front until we hit a live one or end
	n := 0
	for _, tk := range *index {
		if _, err := b.cache.Get(tk.Key); err != nil {
			n++
		} else {
			break
		}
	}
	if n > 0 {
		*index = (*index)[n:]
	}
}

func (b *InMemoryBuffer) RemoveMetricsInRange(start, end time.Time) {
	b.mu.Lock()
	defer b.mu.Unlock()
	
	s := start.UnixNano()
	e := end.UnixNano()
	
	newIndex := make([]TimeKey, 0)
	for _, tk := range b.metricIndex {
		if tk.Timestamp >= s && tk.Timestamp <= e {
			b.cache.Delete(tk.Key)
		} else {
			newIndex = append(newIndex, tk)
		}
	}
	b.metricIndex = newIndex
}

func (b *InMemoryBuffer) RemoveLogsInRange(start, end time.Time) {
	b.mu.Lock()
	defer b.mu.Unlock()
	
	s := start.UnixNano()
	e := end.UnixNano()
	
	newIndex := make([]TimeKey, 0)
	for _, tk := range b.logIndex {
		if tk.Timestamp >= s && tk.Timestamp <= e {
			b.cache.Delete(tk.Key)
		} else {
			newIndex = append(newIndex, tk)
		}
	}
	b.logIndex = newIndex
}

func (b *InMemoryBuffer) ClearMetrics() {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, tk := range b.metricIndex {
		b.cache.Delete(tk.Key)
	}
	b.metricIndex = make([]TimeKey, 0)
}

func (b *InMemoryBuffer) ClearLogs() {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, tk := range b.logIndex {
		b.cache.Delete(tk.Key)
	}
	b.logIndex = make([]TimeKey, 0)
}