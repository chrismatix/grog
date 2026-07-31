package cmds

import (
	"strings"
	"testing"

	"grog/internal/dag"
	"grog/internal/label"
	"grog/internal/model"
)

func TestRenderAgentsFragmentGroupsSortedTargets(t *testing.T) {
	graph := dag.NewDirectedGraphFromMap(model.BuildNodeMap{
		label.TargetLabel{Package: "app", Name: "test"}: &model.Target{
			Label: label.TargetLabel{Package: "app", Name: "test"},
			Tags:  []string{model.TagTestOnly},
		},
		label.TargetLabel{Package: "app", Name: "build"}: &model.Target{
			Label: label.TargetLabel{Package: "app", Name: "build"},
		},
		label.TargetLabel{Package: "cmd", Name: "tool"}: &model.Target{
			Label:     label.TargetLabel{Package: "cmd", Name: "tool"},
			BinOutput: model.NewOutput("bin", "tool"),
		},
	})

	fragment := renderAgentsFragment(graph)

	for _, expected := range []string{
		"### Build targets\n\n- `//app:build`",
		"### Test targets\n\n- `//app:test`",
		"### Binary targets\n\n- `//cmd:tool`",
		"`grog build //...`",
		"`grog test //...`",
		"`grog verify`",
	} {
		if !strings.Contains(fragment, expected) {
			t.Errorf("expected fragment to contain %q:\n%s", expected, fragment)
		}
	}
}

func TestReplaceAgentsFragment(t *testing.T) {
	testCases := []struct {
		name            string
		existingContent string
		fragment        string
		expectedContent string
		expectError     bool
	}{
		{
			name:            "creates new file content",
			fragment:        agentsFragmentStart + "\nnew\n" + agentsFragmentEnd + "\n",
			expectedContent: agentsFragmentStart + "\nnew\n" + agentsFragmentEnd + "\n",
		},
		{
			name:            "appends to existing instructions",
			existingContent: "# Existing\n",
			fragment:        agentsFragmentStart + "\nnew\n" + agentsFragmentEnd + "\n",
			expectedContent: "# Existing\n\n" + agentsFragmentStart + "\nnew\n" + agentsFragmentEnd + "\n",
		},
		{
			name:            "replaces managed content",
			existingContent: "# Existing\n\n" + agentsFragmentStart + "\nold\n" + agentsFragmentEnd + "\n",
			fragment:        agentsFragmentStart + "\nnew\n" + agentsFragmentEnd + "\n",
			expectedContent: "# Existing\n\n" + agentsFragmentStart + "\nnew\n" + agentsFragmentEnd + "\n",
		},
		{
			name:            "rejects incomplete marker",
			existingContent: agentsFragmentStart + "\nold\n",
			fragment:        agentsFragmentStart + "\nnew\n" + agentsFragmentEnd + "\n",
			expectError:     true,
		},
		{
			name:            "rejects reversed markers",
			existingContent: agentsFragmentEnd + "\nold\n" + agentsFragmentStart,
			fragment:        agentsFragmentStart + "\nnew\n" + agentsFragmentEnd + "\n",
			expectError:     true,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			actualContent, err := replaceAgentsFragment(testCase.existingContent, testCase.fragment)
			if testCase.expectError {
				if err == nil {
					t.Fatal("expected an error")
				}
				return
			}
			if err != nil {
				t.Fatalf("replaceAgentsFragment returned an error: %v", err)
			}
			if actualContent != testCase.expectedContent {
				t.Errorf("expected %q, got %q", testCase.expectedContent, actualContent)
			}
		})
	}
}
