package completions

import (
	"github.com/spf13/cobra"
	"grog/internal/config"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
)

func TestTargetPatternCompletionsAll(t *testing.T) {
	cases := []struct {
		workingDirectory string
		input            string
		expect           []string
	}{
		{"", "", []string{"//:bin", "//backend", "//package_1", "//package_2"}},
		{"", "//package_1/", []string{
			"//package_1/nested",
			"//package_1:bar",
			"//package_1:foo",
			"//package_1:foo_foo",
			"//package_1:foo_test",
		}},
		{"package_1", "", []string{
			"//package_1/nested",
			"//package_1:bar",
			"//package_1:foo",
			"//package_1:foo_foo",
			"//package_1:foo_test",
		}},
		{"package_1", "nes", []string{"//package_1/nested"}},
		{"package_1", "foo", []string{"//package_1:foo", "//package_1:foo_foo", "//package_1:foo_test"}},
		{"package_1", "foo_", []string{"//package_1:foo_foo", "//package_1:foo_test"}},
		{"package_1", "foo_test", []string{"//package_1:foo_test"}},
		{"package_1", ":fo", []string{":foo", ":foo_foo", ":foo_test"}},
		{"package_1", ":ba", []string{":bar"}},
		{"", "//package_1/nested:", []string{"//package_1/nested:nested"}},
		{"", "back", []string{"//backend"}},
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

			if err := os.Chdir(filepath.Join(testRepoPath, c.workingDirectory)); err != nil {
				t.Fatalf("chdir failed: %v", err)
			}

			res, _ := AllTargetPatternCompletion(&cobra.Command{}, nil, c.input)
			sort.Strings(res)
			if !reflect.DeepEqual(res, c.expect) {
				t.Errorf("completion(%q)=%v; want %v", c.input, res, c.expect)
			}
		})
	}
}
