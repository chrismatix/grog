package tracing

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"grog/internal/caching/backends"
	gen "grog/internal/proto/gen"

	"google.golang.org/protobuf/proto"
)

const (
	tracesIndexPath = "traces"
	tracesIndexKey  = "index"
	tracesDataPath  = "traces/data"
)

// TraceStore reads and writes build traces via a CacheBackend.
type TraceStore struct {
	backend backends.CacheBackend
}

func NewTraceStore(backend backends.CacheBackend) *TraceStore {
	return &TraceStore{backend: backend}
}

func datePath(t time.Time) string {
	return fmt.Sprintf("%s/%s", tracesDataPath, t.UTC().Format("2006-01-02"))
}

// Write persists a BuildTrace and updates the index.
func (s *TraceStore) Write(ctx context.Context, trace *gen.BuildTrace) error {
	data, err := proto.MarshalOptions{Deterministic: true}.Marshal(trace)
	if err != nil {
		return fmt.Errorf("marshal trace: %w", err)
	}

	traceDate := time.UnixMilli(trace.StartTimeUnixMillis).UTC()
	path := datePath(traceDate)

	if err := s.backend.Set(ctx, path, trace.TraceId, bytes.NewReader(data)); err != nil {
		return fmt.Errorf("write trace: %w", err)
	}

	// Update index (best-effort — trace data is already persisted)
	if err := s.appendToIndex(ctx, trace); err != nil {
		return fmt.Errorf("update index: %w", err)
	}

	return nil
}

func (s *TraceStore) appendToIndex(ctx context.Context, trace *gen.BuildTrace) error {
	index, _ := s.loadIndex(ctx) // ignore error — start fresh if missing
	if index == nil {
		index = &gen.TraceIndex{}
	}

	index.Entries = append(index.Entries, &gen.TraceIndexEntry{
		TraceId:              trace.TraceId,
		StartTimeUnixMillis:  trace.StartTimeUnixMillis,
		Command:              trace.Command,
		TotalTargets:         trace.TotalTargets,
		FailureCount:         trace.FailureCount,
		CacheHitCount:        trace.CacheHitCount,
		TotalDurationMillis:  trace.TotalDurationMillis,
		GitCommit:            trace.GitCommit,
		IsCi:                 trace.IsCi,
	})

	data, err := proto.MarshalOptions{Deterministic: true}.Marshal(index)
	if err != nil {
		return err
	}

	return s.backend.Set(ctx, tracesIndexPath, tracesIndexKey, bytes.NewReader(data))
}

func (s *TraceStore) loadIndex(ctx context.Context) (*gen.TraceIndex, error) {
	reader, err := s.backend.Get(ctx, tracesIndexPath, tracesIndexKey)
	if err != nil {
		return nil, err
	}
	defer reader.Close()

	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}

	var index gen.TraceIndex
	if err := proto.Unmarshal(data, &index); err != nil {
		return nil, err
	}

	return &index, nil
}

// List returns index entries sorted by start time (newest first).
func (s *TraceStore) List(ctx context.Context) ([]*gen.TraceIndexEntry, error) {
	index, err := s.loadIndex(ctx)
	if err != nil {
		return nil, err
	}

	entries := index.Entries
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].StartTimeUnixMillis > entries[j].StartTimeUnixMillis
	})

	return entries, nil
}

// Load retrieves a full BuildTrace by its ID.
// The date string is used to locate the trace in the date-partitioned layout.
func (s *TraceStore) Load(ctx context.Context, traceID string, date string) (*gen.BuildTrace, error) {
	path := fmt.Sprintf("%s/%s", tracesDataPath, date)
	reader, err := s.backend.Get(ctx, path, traceID)
	if err != nil {
		return nil, fmt.Errorf("load trace %s: %w", traceID, err)
	}
	defer reader.Close()

	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}

	var trace gen.BuildTrace
	if err := proto.Unmarshal(data, &trace); err != nil {
		return nil, err
	}

	return &trace, nil
}

// FindAndLoad searches the index for a trace ID (prefix match) and loads it.
func (s *TraceStore) FindAndLoad(ctx context.Context, traceIDPrefix string) (*gen.BuildTrace, error) {
	entries, err := s.List(ctx)
	if err != nil {
		return nil, err
	}

	var match *gen.TraceIndexEntry
	for _, entry := range entries {
		if strings.HasPrefix(entry.TraceId, traceIDPrefix) {
			if match != nil {
				return nil, fmt.Errorf("ambiguous trace ID prefix %q: matches %s and %s", traceIDPrefix, match.TraceId, entry.TraceId)
			}
			match = entry
		}
	}

	if match == nil {
		return nil, fmt.Errorf("no trace found matching %q", traceIDPrefix)
	}

	date := time.UnixMilli(match.StartTimeUnixMillis).UTC().Format("2006-01-02")
	return s.Load(ctx, match.TraceId, date)
}

// Prune deletes traces older than the given time and rebuilds the index.
func (s *TraceStore) Prune(ctx context.Context, olderThan time.Time) (int, error) {
	index, err := s.loadIndex(ctx)
	if err != nil {
		return 0, err
	}

	var kept []*gen.TraceIndexEntry
	pruned := 0

	for _, entry := range index.Entries {
		entryTime := time.UnixMilli(entry.StartTimeUnixMillis)
		if entryTime.Before(olderThan) {
			date := entryTime.UTC().Format("2006-01-02")
			path := fmt.Sprintf("%s/%s", tracesDataPath, date)
			_ = s.backend.Delete(ctx, path, entry.TraceId) // best-effort
			pruned++
		} else {
			kept = append(kept, entry)
		}
	}

	// Rebuild index
	newIndex := &gen.TraceIndex{Entries: kept}
	data, err := proto.MarshalOptions{Deterministic: true}.Marshal(newIndex)
	if err != nil {
		return pruned, err
	}

	if err := s.backend.Set(ctx, tracesIndexPath, tracesIndexKey, bytes.NewReader(data)); err != nil {
		return pruned, err
	}

	return pruned, nil
}
