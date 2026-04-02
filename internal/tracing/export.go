package tracing

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"
)

// ExportJSONL writes each BuildTrace as a single JSON line.
func ExportJSONL(traces []*BuildTrace, w io.Writer) error {
	encoder := json.NewEncoder(w)
	for _, trace := range traces {
		// Flatten into a single JSON object with build fields + spans array
		obj := map[string]any{
			"trace_id":                  trace.Build.TraceID,
			"workspace":                 trace.Build.Workspace,
			"git_commit":                trace.Build.GitCommit,
			"git_branch":                trace.Build.GitBranch,
			"grog_version":              trace.Build.GrogVersion,
			"platform":                  trace.Build.Platform,
			"command":                   trace.Build.Command,
			"start_time_unix_millis":    trace.Build.StartTimeUnixMillis,
			"total_duration_millis":     trace.Build.TotalDurationMillis,
			"total_targets":             trace.Build.TotalTargets,
			"success_count":             trace.Build.SuccessCount,
			"failure_count":             trace.Build.FailureCount,
			"cache_hit_count":           trace.Build.CacheHitCount,
			"critical_path_exec_millis": trace.Build.CriticalPathExecMillis,
			"critical_path_cache_millis": trace.Build.CriticalPathCacheMillis,
			"async_cache_wait_millis":   trace.Build.AsyncCacheWaitMillis,
			"is_ci":                     trace.Build.IsCI,
			"requested_patterns":        trace.Build.RequestedPatterns,
			"spans":                     trace.Spans,
		}
		if err := encoder.Encode(obj); err != nil {
			return fmt.Errorf("encode trace %s: %w", trace.Build.TraceID, err)
		}
	}
	return nil
}

// ExportOTLP writes traces in OpenTelemetry-compatible JSON format.
func ExportOTLP(traces []*BuildTrace, w io.Writer) error {
	export := otlpExport{
		ResourceSpans: make([]otlpResourceSpans, 0, len(traces)),
	}

	for _, trace := range traces {
		rs := buildOTLPResourceSpans(trace)
		export.ResourceSpans = append(export.ResourceSpans, rs)
	}

	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(export)
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
