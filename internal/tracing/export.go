package tracing

import (
	"bufio"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"
)

// SpanLoader loads spans for a batch of traces in one query.
// Satisfied by *TraceStore.
type SpanLoader interface {
	LoadSpansForTraces(ctx context.Context, traceIDs []string) (map[string][]SpanRow, error)
}

// exportChunkSize bounds how many traces' spans are held in memory at once.
const exportChunkSize = 200

func buildJSONLObj(b BuildRow, spans []SpanRow) map[string]any {
	return map[string]any{
		"trace_id":                   b.TraceID,
		"workspace":                  b.Workspace,
		"git_commit":                 b.GitCommit,
		"git_branch":                 b.GitBranch,
		"grog_version":               b.GrogVersion,
		"platform":                   b.Platform,
		"command":                    b.Command,
		"start_time_unix_millis":     b.StartTimeUnixMillis,
		"total_duration_millis":      b.TotalDurationMillis,
		"total_targets":              b.TotalTargets,
		"success_count":              b.SuccessCount,
		"failure_count":              b.FailureCount,
		"cache_hit_count":            b.CacheHitCount,
		"critical_path_exec_millis":  b.CriticalPathExecMillis,
		"critical_path_cache_millis": b.CriticalPathCacheMillis,
		"async_cache_wait_millis":    b.AsyncCacheWaitMillis,
		"is_ci":                      b.IsCI,
		"requested_patterns":         b.RequestedPatterns,
		"spans":                      spans,
	}
}

// ExportJSONL streams traces as newline-delimited JSON, loading spans in
// chunks so memory stays bounded regardless of how many builds are exported.
func ExportJSONL(ctx context.Context, loader SpanLoader, builds []BuildRow, w io.Writer) error {
	bw := bufio.NewWriter(w)
	defer bw.Flush()
	encoder := json.NewEncoder(bw)

	return forEachChunk(ctx, loader, builds, func(b BuildRow, spans []SpanRow) error {
		if err := encoder.Encode(buildJSONLObj(b, spans)); err != nil {
			return fmt.Errorf("encode trace %s: %w", b.TraceID, err)
		}
		return nil
	})
}

// ExportOTLP streams traces as an OTLP JSON document. The top-level object
// must be emitted as a single value, so spans are still loaded chunk-by-chunk
// but each resourceSpans entry is written to the buffered writer as it is
// produced to avoid holding every OTLP span in memory.
func ExportOTLP(ctx context.Context, loader SpanLoader, builds []BuildRow, w io.Writer) error {
	bw := bufio.NewWriter(w)
	defer bw.Flush()

	if _, err := bw.WriteString("{\n  \"resourceSpans\": ["); err != nil {
		return err
	}

	first := true
	err := forEachChunk(ctx, loader, builds, func(b BuildRow, spans []SpanRow) error {
		rs := buildOTLPResourceSpans(&BuildTrace{Build: b, Spans: spans})
		data, err := json.MarshalIndent(rs, "    ", "  ")
		if err != nil {
			return fmt.Errorf("encode trace %s: %w", b.TraceID, err)
		}
		if first {
			if _, err := bw.WriteString("\n    "); err != nil {
				return err
			}
			first = false
		} else {
			if _, err := bw.WriteString(",\n    "); err != nil {
				return err
			}
		}
		_, err = bw.Write(data)
		return err
	})
	if err != nil {
		return err
	}

	_, err = bw.WriteString("\n  ]\n}\n")
	return err
}

func forEachChunk(ctx context.Context, loader SpanLoader, builds []BuildRow, emit func(BuildRow, []SpanRow) error) error {
	for start := 0; start < len(builds); start += exportChunkSize {
		end := start + exportChunkSize
		if end > len(builds) {
			end = len(builds)
		}
		chunk := builds[start:end]

		ids := make([]string, len(chunk))
		for i, b := range chunk {
			ids[i] = b.TraceID
		}
		spansByID, err := loader.LoadSpansForTraces(ctx, ids)
		if err != nil {
			return fmt.Errorf("load spans: %w", err)
		}
		for _, b := range chunk {
			if err := emit(b, spansByID[b.TraceID]); err != nil {
				return err
			}
		}
	}
	return nil
}

// OTLP JSON structures

type otlpExport struct {
	ResourceSpans []otlpResourceSpans `json:"resourceSpans"`
}

type otlpResourceSpans struct {
	Resource   otlpResource     `json:"resource"`
	ScopeSpans []otlpScopeSpans `json:"scopeSpans"`
}

type otlpResource struct {
	Attributes []otlpKeyValue `json:"attributes"`
}

type otlpScopeSpans struct {
	Scope otlpScope  `json:"scope"`
	Spans []otlpSpan `json:"spans"`
}

type otlpScope struct {
	Name    string `json:"name"`
	Version string `json:"version,omitempty"`
}

type otlpSpan struct {
	TraceID           string         `json:"traceId"`
	SpanID            string         `json:"spanId"`
	ParentSpanID      string         `json:"parentSpanId,omitempty"`
	Name              string         `json:"name"`
	Kind              int            `json:"kind"`
	StartTimeUnixNano string         `json:"startTimeUnixNano"`
	EndTimeUnixNano   string         `json:"endTimeUnixNano"`
	Attributes        []otlpKeyValue `json:"attributes"`
	Status            *otlpStatus    `json:"status,omitempty"`
}

type otlpStatus struct {
	Code    int    `json:"code"`
	Message string `json:"message,omitempty"`
}

type otlpKeyValue struct {
	Key   string    `json:"key"`
	Value otlpValue `json:"value"`
}

type otlpValue struct {
	StringValue *string `json:"stringValue,omitempty"`
	IntValue    *string `json:"intValue,omitempty"`
	BoolValue   *bool   `json:"boolValue,omitempty"`
}

func stringVal(s string) otlpValue { return otlpValue{StringValue: &s} }
func intVal(i int64) otlpValue     { s := fmt.Sprintf("%d", i); return otlpValue{IntValue: &s} }
func boolVal(b bool) otlpValue     { return otlpValue{BoolValue: &b} }

func buildOTLPResourceSpans(trace *BuildTrace) otlpResourceSpans {
	traceIDBytes := uuidToBytes(trace.Build.TraceID)
	traceID := hex.EncodeToString(traceIDBytes)

	rootSpanID := generateSpanID(trace.Build.TraceID, "root")
	startNano := fmt.Sprintf("%d", time.UnixMilli(trace.Build.StartTimeUnixMillis).UnixNano())
	endNano := fmt.Sprintf("%d", time.UnixMilli(trace.Build.StartTimeUnixMillis+trace.Build.TotalDurationMillis).UnixNano())

	rootSpan := otlpSpan{
		TraceID:           traceID,
		SpanID:            rootSpanID,
		Name:              fmt.Sprintf("grog %s", trace.Build.Command),
		Kind:              1,
		StartTimeUnixNano: startNano,
		EndTimeUnixNano:   endNano,
		Attributes: []otlpKeyValue{
			{Key: "grog.workspace", Value: stringVal(trace.Build.Workspace)},
			{Key: "grog.git_commit", Value: stringVal(trace.Build.GitCommit)},
			{Key: "grog.git_branch", Value: stringVal(trace.Build.GitBranch)},
			{Key: "grog.total_targets", Value: intVal(int64(trace.Build.TotalTargets))},
			{Key: "grog.cache_hit_count", Value: intVal(int64(trace.Build.CacheHitCount))},
			{Key: "grog.failure_count", Value: intVal(int64(trace.Build.FailureCount))},
			{Key: "grog.is_ci", Value: boolVal(trace.Build.IsCI)},
		},
		Status: &otlpStatus{Code: 1},
	}

	if trace.Build.FailureCount > 0 {
		rootSpan.Status = &otlpStatus{Code: 2, Message: fmt.Sprintf("%d targets failed", trace.Build.FailureCount)}
	}

	spans := []otlpSpan{rootSpan}

	for i, s := range trace.Spans {
		spanID := generateSpanID(trace.Build.TraceID, fmt.Sprintf("span-%d", i))
		sStartNano := fmt.Sprintf("%d", time.UnixMilli(s.StartTimeUnixMillis).UnixNano())
		sEndNano := fmt.Sprintf("%d", time.UnixMilli(s.EndTimeUnixMillis).UnixNano())

		status := &otlpStatus{Code: 1}
		if s.Status == "FAILURE" {
			status = &otlpStatus{Code: 2}
		}

		childSpan := otlpSpan{
			TraceID:           traceID,
			SpanID:            spanID,
			ParentSpanID:      rootSpanID,
			Name:              s.Label,
			Kind:              1,
			StartTimeUnixNano: sStartNano,
			EndTimeUnixNano:   sEndNano,
			Attributes: []otlpKeyValue{
				{Key: "grog.cache_result", Value: stringVal(s.CacheResult)},
				{Key: "grog.change_hash", Value: stringVal(s.ChangeHash)},
				{Key: "grog.is_test", Value: boolVal(s.IsTest)},
				{Key: "grog.command_duration_ms", Value: intVal(s.CommandDurationMillis)},
				{Key: "grog.queue_wait_ms", Value: intVal(s.QueueWaitMillis)},
				{Key: "grog.hash_duration_ms", Value: intVal(s.HashDurationMillis)},
				{Key: "grog.output_write_ms", Value: intVal(s.OutputWriteMillis)},
				{Key: "grog.output_load_ms", Value: intVal(s.OutputLoadMillis)},
				{Key: "grog.cache_write_ms", Value: intVal(s.CacheWriteMillis)},
			},
			Status: status,
		}
		spans = append(spans, childSpan)
	}

	return otlpResourceSpans{
		Resource: otlpResource{
			Attributes: []otlpKeyValue{
				{Key: "service.name", Value: stringVal("grog")},
				{Key: "service.version", Value: stringVal(trace.Build.GrogVersion)},
				{Key: "host.arch", Value: stringVal(trace.Build.Platform)},
			},
		},
		ScopeSpans: []otlpScopeSpans{
			{
				Scope: otlpScope{Name: "grog", Version: trace.Build.GrogVersion},
				Spans: spans,
			},
		},
	}
}

func uuidToBytes(uuidStr string) []byte {
	clean := strings.ReplaceAll(uuidStr, "-", "")
	b, err := hex.DecodeString(clean)
	if err != nil || len(b) != 16 {
		b = make([]byte, 16)
		for i, c := range uuidStr {
			b[i%16] ^= byte(c)
		}
	}
	return b
}

func generateSpanID(traceID string, suffix string) string {
	b := make([]byte, 8)
	combined := traceID + suffix
	for i, c := range combined {
		b[i%8] ^= byte(c)
	}
	return hex.EncodeToString(b)
}
