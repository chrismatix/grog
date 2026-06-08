package tracing

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

type fakeSpanLoader struct {
	byID map[string][]SpanRow
	err  error
}

func (f *fakeSpanLoader) LoadSpansForTraces(_ context.Context, traceIDs []string) (map[string][]SpanRow, error) {
	if f.err != nil {
		return nil, f.err
	}
	out := make(map[string][]SpanRow)
	for _, id := range traceIDs {
		out[id] = f.byID[id]
	}
	return out, nil
}

func TestBuildJSONLObj(t *testing.T) {
	o := buildJSONLObj(BuildRow{TraceID: "a"}, []SpanRow{{TraceID: "a"}})
	if o["trace_id"] != "a" {
		t.Fatal("trace_id")
	}
}

func TestExportJSONL(t *testing.T) {
	loader := &fakeSpanLoader{
		byID: map[string][]SpanRow{
			"a": {{TraceID: "a", Label: "//x:y"}},
			"b": {{TraceID: "b", Label: "//z:w"}},
		},
	}
	var buf bytes.Buffer
	if err := ExportJSONL(context.Background(), loader, []BuildRow{{TraceID: "a"}, {TraceID: "b"}}, &buf); err != nil {
		t.Fatalf("ExportJSONL: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, `"trace_id":"a"`) || !strings.Contains(out, `"trace_id":"b"`) {
		t.Fatalf("got %q", out)
	}
}

func TestExportJSONL_LoaderError(t *testing.T) {
	loader := &fakeSpanLoader{err: errors.New("boom")}
	var buf bytes.Buffer
	if err := ExportJSONL(context.Background(), loader, []BuildRow{{TraceID: "a"}}, &buf); err == nil {
		t.Fatal("expected err")
	}
}

func TestExportOTLP(t *testing.T) {
	loader := &fakeSpanLoader{
		byID: map[string][]SpanRow{
			"a": {{TraceID: "a", Label: "//x:y", StartTimeUnixMillis: 1, EndTimeUnixMillis: 2, Status: "SUCCESS", CacheResult: "CACHE_HIT"}},
			"b": {{TraceID: "b", Label: "//z:w", StartTimeUnixMillis: 3, EndTimeUnixMillis: 4}},
		},
	}
	var buf bytes.Buffer
	if err := ExportOTLP(context.Background(), loader, []BuildRow{
		{TraceID: "a", StartTimeUnixMillis: 1, TotalDurationMillis: 100},
		{TraceID: "b", StartTimeUnixMillis: 2, TotalDurationMillis: 200},
	}, &buf); err != nil {
		t.Fatalf("ExportOTLP: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(buf.Bytes(), &doc); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, buf.String())
	}
	if _, ok := doc["resourceSpans"]; !ok {
		t.Fatalf("missing resourceSpans: %s", buf.String())
	}
}

func TestExportOTLP_LoaderError(t *testing.T) {
	loader := &fakeSpanLoader{err: errors.New("boom")}
	var buf bytes.Buffer
	if err := ExportOTLP(context.Background(), loader, []BuildRow{{TraceID: "a"}}, &buf); err == nil {
		t.Fatal("expected err")
	}
}

func TestStringIntBoolVal(t *testing.T) {
	if v := stringVal("hi"); v.StringValue == nil || *v.StringValue != "hi" {
		t.Fatal("string")
	}
	if v := intVal(42); v.IntValue == nil {
		t.Fatal("int")
	}
	if v := boolVal(true); v.BoolValue == nil || *v.BoolValue != true {
		t.Fatal("bool")
	}
}

func TestUUIDToBytes(t *testing.T) {
	out := uuidToBytes("550e8400-e29b-41d4-a716-446655440000")
	if len(out) == 0 {
		t.Fatal("empty")
	}
	if len(uuidToBytes("not-a-uuid")) == 0 {
		t.Fatal("hash fallback for invalid uuid")
	}
}

func TestGenerateSpanID(t *testing.T) {
	id := generateSpanID("trace", "span")
	if len(id) == 0 {
		t.Fatal("empty")
	}
	id2 := generateSpanID("trace", "span")
	if string(id) != string(id2) {
		t.Fatal("expected deterministic")
	}
}
