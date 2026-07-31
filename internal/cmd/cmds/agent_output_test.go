package cmds

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"grog/internal/dag"
	"grog/internal/execution"
	"grog/internal/failurehistory"
	"grog/internal/label"
	"grog/internal/model"
)

func TestTailDeduplicatedLines(t *testing.T) {
	output := "ignored\nrepeat\nkeep\nrepeat\nlast\n"
	actual := tailDeduplicatedLines(output, 4)
	expected := []string{"repeat", "keep", "last"}
	if strings.Join(actual, "\n") != strings.Join(expected, "\n") {
		t.Errorf("expected %v, got %v", expected, actual)
	}
}

func TestRenderAgentFailuresGroupsAndCapsFailures(t *testing.T) {
	firstLabel := label.TargetLabel{Package: "app", Name: "first"}
	secondLabel := label.TargetLabel{Package: "app", Name: "second"}
	thirdLabel := label.TargetLabel{Package: "app", Name: "third"}
	graph := dag.NewDirectedGraphFromTargets(
		&model.Target{Label: firstLabel, Command: "go test ./app"},
		&model.Target{Label: secondLabel, Command: "go test ./app"},
		&model.Target{Label: thirdLabel, Command: "go test ./other"},
	)
	completionMap := dag.CompletionMap{
		firstLabel: {
			Err: &execution.CommandError{
				ExitCode: 1,
				Output:   "old\nsame\nlast",
				InputChanges: []failurehistory.InputChange{
					{Path: "app/source.go", Kind: "modified"},
				},
			},
		},
		secondLabel: {
			Err: &execution.CommandError{
				ExitCode: 1,
				Output:   "old\nsame\nlast",
				InputChanges: []failurehistory.InputChange{
					{Path: "app/source.go", Kind: "modified"},
				},
			},
		},
		thirdLabel: {
			Err: errors.New("setup failed"),
		},
	}

	var buffer bytes.Buffer
	renderAgentFailures(&buffer, completionMap, graph, 2, 2)
	output := buffer.String()

	if !strings.Contains(output, "FAIL //app:first, //app:second (exit 1)") {
		t.Errorf("expected grouped labels, got:\n%s", output)
	}
	if strings.Contains(output, "old") {
		t.Errorf("expected output tail to exclude old lines, got:\n%s", output)
	}
	if !strings.Contains(output, "same\nlast") {
		t.Errorf("expected final output lines, got:\n%s", output)
	}
	if !strings.Contains(output, "1 more failing targets omitted") {
		t.Errorf("expected omitted failure count, got:\n%s", output)
	}
	if !strings.Contains(output, "changes since last green:\nmodified app/source.go") {
		t.Errorf("expected input changes, got:\n%s", output)
	}
}
