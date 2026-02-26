package execution

import (
	"strings"
	"testing"

	"grog/internal/proto/gen"
)

func TestFormatTargetResultForDebugWithNilTargetResult(t *testing.T) {
	formattedTargetResult := formatTargetResultForDebug(nil)
	if formattedTargetResult != "<nil>" {
		t.Fatalf("expected <nil> for nil target result but got %q", formattedTargetResult)
	}
}

func TestFormatTargetResultForDebugIncludesEmptyOutputsArray(t *testing.T) {
	targetResult := &gen.TargetResult{
		ChangeHash: "abc123",
	}

	formattedTargetResult := formatTargetResultForDebug(targetResult)

	if !strings.Contains(formattedTargetResult, "\"outputs\":[]") {
		t.Fatalf("expected formatted target result to include an empty outputs array but got %q", formattedTargetResult)
	}
}
