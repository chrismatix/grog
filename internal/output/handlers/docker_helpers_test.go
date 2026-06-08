package handlers

import (
	"context"
	"strings"
	"testing"

	"grog/internal/config"
)

func TestLoopbackRepoName(t *testing.T) {
	got := loopbackRepoName("sha256:abc123")
	if !strings.HasPrefix(got, "grog-cache/") {
		t.Fatalf("got %q", got)
	}
}

func TestShortID(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"", "unknown"},
		{"sha256:", "unknown"},
		{"sha256:abc", "abc"},
		{"sha256:" + strings.Repeat("a", 64), strings.Repeat("a", 32)},
		{strings.Repeat("b", 40), strings.Repeat("b", 32)},
	}
	for _, c := range cases {
		if got := shortID(c.in); got != c.want {
			t.Fatalf("in=%q got %q want %q", c.in, got, c.want)
		}
	}
}

func TestDockerOutputHandler_Type(t *testing.T) {
	h := NewDockerOutputHandler(context.Background(), nil)
	if h.Type() != OCIHandler {
		t.Fatalf("type %v", h.Type())
	}
	if err := h.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestDockerRegistryOutputHandler_TypeAndImageName(t *testing.T) {
	prev := config.Global
	config.Global = config.WorkspaceConfig{WorkspaceRoot: "/tmp/abc"}
	t.Cleanup(func() { config.Global = prev })

	h := NewDockerRegistryOutputHandler(nil, config.OCIConfig{Registry: "gcr.io/x"})
	if h.Type() != OCIHandler {
		t.Fatalf("type %v", h.Type())
	}
	name := h.cacheImageName("sha256:deadbeef")
	if !strings.HasPrefix(name, "gcr.io/x/") {
		t.Fatalf("got %q", name)
	}
	if !strings.HasSuffix(name, "-deadbeef") {
		t.Fatalf("got %q", name)
	}
}

func TestFormatPhaseSummary(t *testing.T) {
	if formatPhaseSummary(nil) != "" {
		t.Fatal("empty")
	}
	got := formatPhaseSummary(map[string]string{
		"a": "Pushing",
		"b": "Pushing",
		"c": "Preparing",
		"d": "Layer already exists",
		"e": "Magic",
	})
	if !strings.Contains(got, "2 pushing") || !strings.Contains(got, "1 cached") || !strings.Contains(got, "magic") {
		t.Fatalf("got %q", got)
	}
}
