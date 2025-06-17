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

func setupCompletionWorkspace(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	config.Global.WorkspaceRoot = root
	config.Global.Root = root
	if err := os.WriteFile(filepath.Join(root, "grog.toml"), []byte{}, 0644); err != nil {
		t.Fatalf("failed to write grog.toml: %v", err)
	}
	return root
}

func writeBuild(path string, content string) {
	os.MkdirAll(filepath.Dir(path), 0755)
	os.WriteFile(path, []byte(content), 0644)
}

func TestTargetPatternCompletion(t *testing.T) {
	root := setupCompletionWorkspace(t)

	// root package
	writeBuild(filepath.Join(root, "BUILD.json"), `{"targets":[{"name":"root","command":"echo root"}]}`)
	// foo package with subpackage bar
	writeBuild(filepath.Join(root, "foo", "BUILD.json"), `{"targets":[{"name":"foo","command":"echo foo"},{"name":"foo_test","command":"echo test"}]}`)
	writeBuild(filepath.Join(root, "foo", "bar", "BUILD.json"), `{"targets":[{"name":"bar","command":"echo bar"}]}`)

	cases := []struct {
		wd     string
		input  string
		expect []string
	}{
		{"", "", []string{"//:root", "//foo/"}},
		{"", "//foo/", []string{"//foo/bar/", "//foo:foo", "//foo:foo_test"}},
		{"foo", "", []string{"//foo/bar/", "//foo:foo", "//foo:foo_test"}},
		{"foo", ":foo", []string{"//foo:foo"}},
		{"foo", ":f", []string{"//foo:foo", "//foo:foo_test"}},
		{"", "//foo/bar:", []string{"//foo/bar:bar"}},
	}

	for _, c := range cases {
		t.Run(c.wd+"-"+c.input, func(t *testing.T) {
			if err := os.Chdir(filepath.Join(root, c.wd)); err != nil {
				t.Fatalf("chdir failed: %v", err)
			}
			res, _ := TargetPatternCompletion(&cobra.Command{}, nil, c.input)
			sort.Strings(res)
			if !reflect.DeepEqual(res, c.expect) {
				t.Errorf("completion(%q)=%v; want %v", c.input, res, c.expect)
			}
		})
	}
}
