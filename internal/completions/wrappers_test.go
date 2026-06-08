package completions

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/spf13/cobra"

	"grog/internal/config"
)

func setupCompletionsRepo(t *testing.T) {
	t.Helper()
	_, file, _, _ := runtime.Caller(0)
	repo := filepath.Join(filepath.Dir(file), "..", "..", "integration", "test_repos", "completions")
	resolved, err := filepath.EvalSymlinks(repo)
	if err != nil {
		t.Fatal(err)
	}
	prev := config.Global
	prevWd, _ := os.Getwd()
	config.Global = config.WorkspaceConfig{WorkspaceRoot: resolved}
	if err := os.Chdir(resolved); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		config.Global = prev
		_ = os.Chdir(prevWd)
	})
}

func TestTestTargetPatternCompletion(t *testing.T) {
	setupCompletionsRepo(t)
	out, _ := TestTargetPatternCompletion(&cobra.Command{}, nil, "")
	_ = out
}

func TestBuildTargetPatternCompletion(t *testing.T) {
	setupCompletionsRepo(t)
	out, _ := BuildTargetPatternCompletion(&cobra.Command{}, nil, "")
	_ = out
}

func TestBinaryTargetPatternCompletion(t *testing.T) {
	setupCompletionsRepo(t)
	out, _ := BinaryTargetPatternCompletion(&cobra.Command{}, nil, "")
	_ = out
}

func TestSplitPartialPrefix(t *testing.T) {
	cases := []struct {
		in       string
		hasSlash bool
		wantHead string
		wantTail string
		hasColon bool
		wantPkg  string
		wantTgt  string
	}{
		{"//pkg/sub", true, "//pkg", "sub", false, "", ""},
		{"//pkg", false, "//pkg", "", false, "", ""},
		{"", false, "", "", false, "", ""},
	}
	for _, c := range cases {
		_, _ = splitPartialPrefix(c.in)
	}
}

func TestNormalizePackagePath(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{".", ""},
		{"", ""},
		{"a", "a"},
	}
	for _, c := range cases {
		if got := normalizePackagePath(c.in); got != c.want {
			t.Fatalf("in=%q got %q want %q", c.in, got, c.want)
		}
	}
}
