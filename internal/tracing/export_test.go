package tracing

import (
	"encoding/json"
	"testing"
)

func TestBuildOTLPResourceSpans_Attributes(t *testing.T) {
	trace := makeTestTrace("6f1c2a4e-0d0a-4a8e-9a43-1d2b3c4d5e6f", 1_700_000_000_000, "build")
	trace.Build.CriticalPath = "//pkg:library,//pkg:target"
	trace.Build.Tags = `{"github.run_id":"123","shard":"2"}`
	trace.Spans[0].Dependencies = "//pkg:library,//other:tool"
	trace.Spans[0].DepLoadMillis = 40

	resourceSpans := buildOTLPResourceSpans(trace)
	spans := resourceSpans.ScopeSpans[0].Spans

	attributesOf := func(span otlpSpan) map[string]string {
		t.Helper()
		attributes := map[string]string{}
		for _, attribute := range span.Attributes {
			encoded, err := json.Marshal(attribute.Value)
			if err != nil {
				t.Fatalf("encode %s: %v", attribute.Key, err)
			}
			attributes[attribute.Key] = string(encoded)
		}
		return attributes
	}

	tests := []struct {
		name       string
		attributes map[string]string
		key        string
		expected   string
	}{
		{"critical path", attributesOf(spans[0]), "grog.critical_path", `{"arrayValue":{"values":[{"stringValue":"//pkg:library"},{"stringValue":"//pkg:target"}]}}`},
		{"github tag", attributesOf(spans[0]), "grog.tag.github.run_id", `{"stringValue":"123"}`},
		{"custom tag", attributesOf(spans[0]), "grog.tag.shard", `{"stringValue":"2"}`},
		{"dependencies", attributesOf(spans[1]), "grog.dependencies", `{"arrayValue":{"values":[{"stringValue":"//pkg:library"},{"stringValue":"//other:tool"}]}}`},
		{"empty target tags", attributesOf(spans[1]), "grog.tags", `{"arrayValue":{"values":[]}}`},
		{"dep load", attributesOf(spans[1]), "grog.dep_load_ms", `{"intValue":"40"}`},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if actual := test.attributes[test.key]; actual != test.expected {
				t.Errorf("expected %s = %s, got %s", test.key, test.expected, actual)
			}
		})
	}
}
