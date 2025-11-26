package completions

import (
	"github.com/spf13/cobra"
	"grog/internal/config"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"testing"
)

func TestTargetPatternCompletionsAll(t *testing.T) {
	if _, err := exec.LookPath("pkl"); err != nil {
		t.Skip("pkl CLI not installed")
	}

	cases := []struct {
		workingDirectory string
		input            string
		expect           []string
	}{
		{"", "", []string{"//:bin", "//package_1", "//package_2"}},
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
		{"", "//package_1/nested:", []string{"//package_1/nested:nested"}},
	}

	_, testFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatalf("could not determine test file location")
	}

	testDir := filepath.Dir(testFile)
	testRepoPath := filepath.Join(testDir, "..", "..", "integration", "test_repos", "completions")

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
