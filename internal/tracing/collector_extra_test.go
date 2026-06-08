package tracing

import (
	"strings"
	"testing"
	"time"

	"grog/internal/dag"
	"grog/internal/label"
	"grog/internal/model"
)

func TestTruncateCommand(t *testing.T) {
	if got := truncateCommand("short"); got != "short" {
		t.Fatal("short")
	}
	long := strings.Repeat("a", maxCommandLen+10)
	if len(truncateCommand(long)) != maxCommandLen {
		t.Fatal("truncated")
	}
}

func TestBuildSpan_StatusVariants(t *testing.T) {
	pattern, err := label.ParseTargetPattern("", "//...")
	if err != nil {
		t.Fatal(err)
	}
	c := NewTraceCollector("build", []label.TargetPattern{pattern}, "0.1.0")

	tgt := &model.Target{
		Label:         label.TL("pkg", "t"),
		Command:       "echo",
		StartTime:     time.Now(),
		ExecutionTime: 100 * time.Millisecond,
		Tags:          []string{"x"},
		Dependencies:  []label.TargetLabel{label.TL("pkg", "d")},
	}

	for _, comp := range []dag.Completion{
		{NodeType: model.TargetNode, IsSuccess: true, CacheResult: dag.CacheHit},
		{NodeType: model.TargetNode, IsSuccess: true, CacheResult: dag.CacheSkip},
		{NodeType: model.TargetNode, IsSuccess: false},
	} {
		span := c.buildSpan(tgt, &comp)
		if span.Label == "" {
			t.Fatal("empty label")
		}
	}
}
