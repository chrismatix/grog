package completions

import (
	"grog/internal/config"
	"grog/internal/selection"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"

	"github.com/spf13/cobra"
)

func TestTargetPatternCompletionsAll(t *testing.T) {
	cases := []struct {
		workingDirectory string
		input            string
		expect           []string
	}{
		{"", "", []string{"//...", "//:...", "//:bin", "//package_1", "//package_1/...", "//package_2"}},
		{"", "//package_1/", []string{
			"//package_1/...",
			"//package_1/nested",
			"//package_1:...",
			"//package_1:bar",
			"//package_1:foo",
			"//package_1:foo_foo",
			"//package_1:foo_test",
		}},
		{"package_1", "", []string{
			"//package_1/...",
			"//package_1/nested",
			"//package_1:...",
			"//package_1:bar",
			"//package_1:foo",
			"//package_1:foo_foo",
			"//package_1:foo_test",
		}},
		{"package_1", "nes", []string{"//package_1/...", "//package_1/nested", "//package_1:..."}},
		{"package_1", "foo", []string{"//package_1/...", "//package_1:...", "//package_1:foo", "//package_1:foo_foo", "//package_1:foo_test"}},
		{"package_1", "foo_", []string{"//package_1/...", "//package_1:...", "//package_1:foo_foo", "//package_1:foo_test"}},
		{"package_1", "foo_test", []string{"//package_1/...", "//package_1:...", "//package_1:foo_test"}},
		{"", "//package_1/nested:", []string{"//package_1/nested:...", "//package_1/nested:nested"}},
		// Test relative target completion (input starts with ":")
		{"package_1", ":foo", []string{":...", ":foo", ":foo_foo", ":foo_test"}},
		{"package_1", ":foo_", []string{":...", ":foo_foo", ":foo_test"}},
		{"package_1", ":", []string{":...", ":bar", ":foo", ":foo_foo", ":foo_test"}},
		// Test partial directory completion from root
		{"", "pack", []string{"//...", "//:...", "//package_1", "//package_1/...", "//package_2"}},
		{"", "package_1", []string{"//...", "//:...", "//package_1", "//package_1/..."}},
		{"", "//pack", []string{"//package_1/", "//package_1/...", "//package_2"}},
		{"", "//package_1", []string{
			"//package_1/...",
			"//package_1/nested",
			"//package_1:...",
			"//package_1:bar",
			"//package_1:foo",
			"//package_1:foo_foo",
			"//package_1:foo_test",
		}},
	}

	testFile, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd failed: %v", err)
	}

	testDir := filepath.Dir(testFile)
	testRepoPath := filepath.Join(testDir, "..", "integration", "test_repos", "completions")

	for _, c := range cases {
		t.Run(c.workingDirectory+"-"+c.input, func(t *testing.T) {
			config.Global.WorkspaceRoot = testRepoPath

			t.Chdir(filepath.Join(testRepoPath, c.workingDirectory))

			res, _ := AllTargetPatternCompletion(&cobra.Command{}, nil, c.input)
			sort.Strings(res)
			if !reflect.DeepEqual(res, c.expect) {
				t.Errorf("completion(%q)=%v; want %v", c.input, res, c.expect)
			}
		})
	}
}

func TestTargetPatternCompletionsByTargetType(t *testing.T) {
	cases := []struct {
		workingDirectory string
		input            string
		targetType       selection.TargetTypeSelection
		expect           []string
	}{
		{"", "//package_2", selection.AllTargets, []string{"//package_2:", "//package_2:...", "//package_2:package_2"}},
		{"", "//package_1/nested", selection.AllTargets, []string{"//package_1/nested:", "//package_1/nested:...", "//package_1/nested:nested"}},
		{"", "//package_1/nes", selection.AllTargets, []string{"//package_1/nested"}},
		{"", "//nope", selection.AllTargets, []string{}},
		{"", ":", selection.AllTargets, []string{":...", ":bin"}},
		{"package_1/nested", "", selection.AllTargets, []string{"//package_1/nested:...", "//package_1/nested:nested"}},
		{"", "//package_1:foo", selection.TestOnly, []string{"//package_1/...", "//package_1:...", "//package_1:foo_test"}},
		{"", "//:", selection.TestOnly, []string{"//...", "//package_1", "//package_1/...", "//package_2"}},
		{"package_1", ":bar", selection.TestOnly, []string{":..."}},
		{"", "//:b", selection.BinOutput, []string{"//...", "//:...", "//:bin"}},
		{"", "//package_1:foo", selection.NonTestOnly, []string{"//package_1/...", "//package_1:...", "//package_1:foo", "//package_1:foo_foo"}},
	}

	testFile, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd failed: %v", err)
	}

	testDir := filepath.Dir(testFile)
	testRepoPath := filepath.Join(testDir, "..", "integration", "test_repos", "completions")

	for _, c := range cases {
		t.Run(c.workingDirectory+"-"+c.input, func(t *testing.T) {
			config.Global.WorkspaceRoot = testRepoPath

			t.Chdir(filepath.Join(testRepoPath, c.workingDirectory))

			res, directive := TargetPatternCompletion(&cobra.Command{}, nil, c.input, c.targetType)
			sort.Strings(res)
			if res == nil {
				res = []string{}
			}
			if !reflect.DeepEqual(res, c.expect) {
				t.Errorf("completion(%q)=%v; want %v", c.input, res, c.expect)
			}
			if directive != cobra.ShellCompDirectiveNoFileComp|cobra.ShellCompDirectiveNoSpace {
				t.Errorf("completion(%q) directive=%d; want NoFileComp|NoSpace", c.input, directive)
			}
		})
	}
}
