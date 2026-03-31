package tracing

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"time"

	gen "grog/internal/proto/gen"

	"google.golang.org/protobuf/encoding/protojson"
)

// ExportJSONL writes each BuildTrace as a single JSON line.
func ExportJSONL(traces []*gen.BuildTrace, w io.Writer) error {
	marshaler := protojson.MarshalOptions{
		EmitUnpopulated: false,
		UseProtoNames:   true,
	}

	for _, trace := range traces {
		data, err := marshaler.Marshal(trace)
		if err != nil {
			return fmt.Errorf("marshal trace %s: %w", trace.TraceId, err)
		}
		if _, err := w.Write(data); err != nil {
			return err
		}
		if _, err := w.Write([]byte("\n")); err != nil {
			return err
		}
	}
	return nil
}

// ExportOTLP writes traces in OpenTelemetry-compatible JSON format (OTLP).
// This is a structural mapping — no OTEL SDK dependency required.
func ExportOTLP(traces []*gen.BuildTrace, w io.Writer) error {
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

// OTLP JSON structures (subset of the OTLP spec sufficient for trace export)

type otlpExport struct {
	ResourceSpans []otlpResourceSpans `json:"resourceSpans"`
}

type otlpResourceSpans struct {
	Resource  otlpResource    `json:"resource"`
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
	TraceID            string         `json:"traceId"`
	SpanID             string         `json:"spanId"`
	ParentSpanID       string         `json:"parentSpanId,omitempty"`
	Name               string         `json:"name"`
	Kind               int            `json:"kind"` // 1=INTERNAL
	StartTimeUnixNano  string         `json:"startTimeUnixNano"`
	EndTimeUnixNano    string         `json:"endTimeUnixNano"`
	Attributes         []otlpKeyValue `json:"attributes"`
	Status             *otlpStatus    `json:"status,omitempty"`
}

type otlpStatus struct {
	Code    int    `json:"code"` // 0=UNSET, 1=OK, 2=ERROR
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

func buildOTLPResourceSpans(trace *gen.BuildTrace) otlpResourceSpans {
	// Generate a 16-byte trace ID from the UUID
	traceIDBytes := uuidToBytes(trace.TraceId)
	traceID := hex.EncodeToString(traceIDBytes)

	// Root span for the build
	rootSpanID := generateSpanID(trace.TraceId, "root")
	startNano := fmt.Sprintf("%d", time.UnixMilli(trace.StartTimeUnixMillis).UnixNano())
	endNano := fmt.Sprintf("%d", time.UnixMilli(trace.StartTimeUnixMillis+trace.TotalDurationMillis).UnixNano())

	rootSpan := otlpSpan{
		TraceID:           traceID,
		SpanID:            rootSpanID,
		Name:              fmt.Sprintf("grog %s", trace.Command),
		Kind:              1,
		StartTimeUnixNano: startNano,
		EndTimeUnixNano:   endNano,
		Attributes: []otlpKeyValue{
			{Key: "grog.workspace", Value: stringVal(trace.Workspace)},
			{Key: "grog.git_commit", Value: stringVal(trace.GitCommit)},
			{Key: "grog.git_branch", Value: stringVal(trace.GitBranch)},
			{Key: "grog.total_targets", Value: intVal(int64(trace.TotalTargets))},
			{Key: "grog.cache_hit_count", Value: intVal(int64(trace.CacheHitCount))},
			{Key: "grog.failure_count", Value: intVal(int64(trace.FailureCount))},
			{Key: "grog.is_ci", Value: boolVal(trace.IsCi)},
		},
		Status: &otlpStatus{Code: 1}, // OK
	}

	if trace.FailureCount > 0 {
		rootSpan.Status = &otlpStatus{Code: 2, Message: fmt.Sprintf("%d targets failed", trace.FailureCount)}
	}

	spans := []otlpSpan{rootSpan}

	// Child spans for each target
	for i, s := range trace.Spans {
		spanID := generateSpanID(trace.TraceId, fmt.Sprintf("span-%d", i))
		sStartNano := fmt.Sprintf("%d", time.UnixMilli(s.StartTimeUnixMillis).UnixNano())
		sEndNano := fmt.Sprintf("%d", time.UnixMilli(s.EndTimeUnixMillis).UnixNano())

		status := &otlpStatus{Code: 1}
		if s.Status == gen.TargetSpan_FAILURE {
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
				{Key: "grog.cache_result", Value: stringVal(s.CacheResult.String())},
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
				{Key: "service.version", Value: stringVal(trace.GrogVersion)},
				{Key: "host.arch", Value: stringVal(trace.Platform)},
			},
		},
		ScopeSpans: []otlpScopeSpans{
			{
				Scope: otlpScope{Name: "grog", Version: trace.GrogVersion},
				Spans: spans,
			},
		},
	}
}

// uuidToBytes converts a UUID string to 16 bytes for OTLP trace ID.
func uuidToBytes(uuidStr string) []byte {
	clean := ""
	for _, c := range uuidStr {
		if c != '-' {
			clean += string(c)
		}
	}
	b, err := hex.DecodeString(clean)
	if err != nil || len(b) != 16 {
		// Fallback: hash the string
		b = make([]byte, 16)
		for i, c := range uuidStr {
			b[i%16] ^= byte(c)
		}
	}
	return b
}

// generateSpanID creates an 8-byte span ID deterministically from trace ID + suffix.
func generateSpanID(traceID string, suffix string) string {
	b := make([]byte, 8)
	combined := traceID + suffix
	for i, c := range combined {
		b[i%8] ^= byte(c)
	}
	return hex.EncodeToString(b)
}
