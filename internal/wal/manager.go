package wal

import (
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	"OTEL_Tail_Sampler/internal/wal/pb"
	"OTEL_Tail_Sampler/pkg/signals"

	"github.com/tidwall/wal"
	"go.opentelemetry.io/collector/pdata/plog"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.opentelemetry.io/collector/pdata/ptrace"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type Log struct {
	mu   sync.Mutex
	log  *wal.Log
	path string

	traceMarshaler    ptrace.ProtoMarshaler
	traceUnmarshaler  ptrace.ProtoUnmarshaler
	metricMarshaler   pmetric.ProtoMarshaler
	metricUnmarshaler pmetric.ProtoUnmarshaler
	logMarshaler      plog.ProtoMarshaler
	logUnmarshaler    plog.ProtoUnmarshaler
}

func Open(path string) (*Log, error) {
	if err := os.MkdirAll(path, 0755); err != nil {
		return nil, fmt.Errorf("failed to create wal dir: %w", err)
	}

	l, err := wal.Open(path, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to open wal: %w", err)
	}

	return &Log{
		log:  l,
		path: path,
	}, nil
}

func (l *Log) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.log.Close()
}

func (l *Log) Write(sig signals.Signal) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	var record pb.WALRecord
	var data []byte
	var err error

	switch s := sig.(type) {
	case *signals.TraceSignal:
		record.Type = pb.WALRecord_TRACE
		data, err = l.traceMarshaler.MarshalTraces(s.Traces)
	case *signals.MetricSignal:
		record.Type = pb.WALRecord_METRIC
		data, err = l.metricMarshaler.MarshalMetrics(s.Metrics)
	case *signals.LogSignal:
		record.Type = pb.WALRecord_LOG
		data, err = l.logMarshaler.MarshalLogs(s.Logs)
	default:
		return fmt.Errorf("unknown signal type")
	}

	if err != nil {
		return fmt.Errorf("failed to marshal pdata: %w", err)
	}

	record.Data = data
	record.Timestamp = timestamppb.New(sig.Context().ReceivedAt)

	recordBytes, err := proto.Marshal(&record)
	if err != nil {
		return fmt.Errorf("failed to marshal wal record: %w", err)
	}

	lastIndex, err := l.log.LastIndex()
	if err != nil {
		return err
	}

	return l.log.Write(lastIndex+1, recordBytes)
}

func (l *Log) Replay(fn func(signals.Signal)) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	first, err := l.log.FirstIndex()
	if err != nil {
		return err
	}
	last, err := l.log.LastIndex()
	if err != nil {
		return err
	}

	for i := first; i <= last; i++ {
		data, err := l.log.Read(i)
		if err != nil {
			log.Printf("Failed to read wal entry %d: %v", i, err)
			continue
		}

		var record pb.WALRecord
		if err := proto.Unmarshal(data, &record); err != nil {
			log.Printf("Failed to unmarshal wal entry %d: %v", i, err)
			continue
		}

		var sig signals.Signal
		ctx := signals.Context{
			ReceivedAt: record.Timestamp.AsTime(),
		}

		switch record.Type {
		case pb.WALRecord_TRACE:
			traces, err := l.traceUnmarshaler.UnmarshalTraces(record.Data)
			if err != nil {
				continue
			}
			sig = &signals.TraceSignal{Traces: traces, Ctx: ctx}
		case pb.WALRecord_METRIC:
			metrics, err := l.metricUnmarshaler.UnmarshalMetrics(record.Data)
			if err != nil {
				continue
			}
			sig = &signals.MetricSignal{Metrics: metrics, Ctx: ctx}
		case pb.WALRecord_LOG:
			logs, err := l.logUnmarshaler.UnmarshalLogs(record.Data)
			if err != nil {
				continue
			}
			sig = &signals.LogSignal{Logs: logs, Ctx: ctx}
		}

		if sig != nil {
			fn(sig)
		}
	}

	return nil
}

// TruncateBefore removes entries older than the given time.
// Since tidwall/wal only supports TruncateFront(index), we scan to find the index.
func (l *Log) TruncateBefore(t time.Time) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	first, err := l.log.FirstIndex()
	if err != nil {
		return err
	}
	last, err := l.log.LastIndex()
	if err != nil {
		return err
	}

	var truncateIndex uint64
	for i := first; i <= last; i++ {
		data, err := l.log.Read(i)
		if err != nil {
			continue
		}
		var record pb.WALRecord
		if err := proto.Unmarshal(data, &record); err != nil {
			continue
		}
		if record.Timestamp.AsTime().Before(t) {
			truncateIndex = i
		} else {
			break
		}
	}

	if truncateIndex > 0 {
		return l.log.TruncateFront(truncateIndex + 1)
	}
	return nil
}
