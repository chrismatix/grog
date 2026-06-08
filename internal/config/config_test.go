package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWorkspaceConfig_IsDebug(t *testing.T) {
	cases := []struct {
		level string
		want  bool
	}{
		{"debug", true},
		{"info", false},
		{"", false},
		{"DEBUG", false},
	}
	for _, c := range cases {
		w := WorkspaceConfig{LogLevel: c.level}
		if got := w.IsDebug(); got != c.want {
			t.Fatalf("IsDebug(%q) = %v, want %v", c.level, got, c.want)
		}
	}
}

func TestWorkspaceConfig_GetPlatform(t *testing.T) {
	w := WorkspaceConfig{OS: "linux", Arch: "amd64"}
	if got := w.GetPlatform(); got != "linux/amd64" {
		t.Fatalf("got %q", got)
	}
}

func TestWorkspaceConfig_GetCasDirectory(t *testing.T) {
	w := WorkspaceConfig{Root: "/grog"}
	if got := w.GetCasDirectory(); got != filepath.Join("/grog", "cas") {
		t.Fatalf("got %q", got)
	}
}

func TestWorkspaceConfig_GetCurrentPackage(t *testing.T) {
	tmp := t.TempDir()
	sub := filepath.Join(tmp, "pkg", "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	resolved, err := filepath.EvalSymlinks(tmp)
	if err != nil {
		t.Fatal(err)
	}
	subResolved := filepath.Join(resolved, "pkg", "sub")

	cwd, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(cwd) })
	if err := os.Chdir(subResolved); err != nil {
		t.Fatal(err)
	}

	w := WorkspaceConfig{WorkspaceRoot: resolved}
	pkg, err := w.GetCurrentPackage()
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if pkg != filepath.Join("pkg", "sub") {
		t.Fatalf("got pkg %q", pkg)
	}

	if err := os.Chdir(resolved); err != nil {
		t.Fatal(err)
	}
	pkg, err = w.GetCurrentPackage()
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if pkg != "" {
		t.Fatalf("want empty, got %q", pkg)
	}
}

func TestWorkspaceConfig_Validate_Success(t *testing.T) {
	cases := []WorkspaceConfig{
		{HashAlgorithm: "", LoadOutputs: "all"},
		{HashAlgorithm: "xxh3", LoadOutputs: "all"},
		{HashAlgorithm: "SHA256", LoadOutputs: "all"},
		{OCI: OCIConfig{Backend: OCIBackendFS}, LoadOutputs: "all"},
		{OCI: OCIConfig{Backend: OCIBackendRegistry}, LoadOutputs: "all"},
		{LoadOutputs: "all"},
		{LoadOutputs: "minimal"},
		{LoadOutputs: "all", OutputMode: ""},
		{LoadOutputs: "all", OutputMode: "terse"},
		{LoadOutputs: "all", OutputMode: "detailed"},
		{LoadOutputs: "all", Tags: []string{"a", "b"}, ExcludeTags: []string{"c"}},
	}
	for i, c := range cases {
		if err := c.Validate(); err != nil {
			t.Fatalf("case %d unexpected err: %v", i, err)
		}
	}
}

func TestWorkspaceConfig_Validate_Errors(t *testing.T) {
	cases := []struct {
		name    string
		cfg     WorkspaceConfig
		wantSub string
	}{
		{"hash", WorkspaceConfig{HashAlgorithm: "md5"}, "invalid hash_algorithm"},
		{"oci", WorkspaceConfig{OCI: OCIConfig{Backend: "weird"}}, "invalid oci backend"},
		{"overlap tags", WorkspaceConfig{Tags: []string{"x"}, ExcludeTags: []string{"x"}}, "cannot both be selected and excluded"},
		{"load_outputs", WorkspaceConfig{LoadOutputs: "wat"}, "invalid load_outputs"},
		{"output_mode", WorkspaceConfig{LoadOutputs: "all", OutputMode: "fancy"}, "invalid output_mode"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := c.cfg.Validate()
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), c.wantSub) {
				t.Fatalf("err %q lacks %q", err, c.wantSub)
			}
		})
	}
}

func TestValidateGrogVersion(t *testing.T) {
	cases := []struct {
		name    string
		require string
		current string
		wantErr bool
	}{
		{"unset", "", "1.0.0", false},
		{"satisfies", ">=1.0.0", "1.2.3", false},
		{"violates", ">=2.0.0", "1.2.3", true},
		{"bad range", "not-a-range", "1.0.0", true},
		{"bad current", ">=1.0.0", "not-semver", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w := WorkspaceConfig{RequiredGrogVersion: c.require}
			err := w.ValidateGrogVersion(c.current)
			if (err != nil) != c.wantErr {
				t.Fatalf("got err=%v, wantErr=%v", err, c.wantErr)
			}
		})
	}
}

func TestGetLoadOutputsMode(t *testing.T) {
	if (WorkspaceConfig{LoadOutputs: "minimal"}).GetLoadOutputsMode() != LoadOutputsMinimal {
		t.Fatal("minimal")
	}
	if (WorkspaceConfig{LoadOutputs: "all"}).GetLoadOutputsMode() != LoadOutputsAll {
		t.Fatal("all")
	}
	if (WorkspaceConfig{LoadOutputs: "bogus"}).GetLoadOutputsMode() != LoadOutputsAll {
		t.Fatal("invalid fallback")
	}
}

func TestGetOutputMode(t *testing.T) {
	if (WorkspaceConfig{OutputMode: "detailed"}).GetOutputMode() != OutputModeDetailed {
		t.Fatal("detailed")
	}
	if (WorkspaceConfig{OutputMode: ""}).GetOutputMode() != OutputModeTerse {
		t.Fatal("terse default")
	}
	if (WorkspaceConfig{OutputMode: "bogus"}).GetOutputMode() != OutputModeTerse {
		t.Fatal("invalid fallback")
	}
}

func TestParseLoadOutputsMode(t *testing.T) {
	if m, err := ParseLoadOutputsMode("all"); err != nil || m != LoadOutputsAll {
		t.Fatalf("all: m=%v err=%v", m, err)
	}
	if m, err := ParseLoadOutputsMode("minimal"); err != nil || m != LoadOutputsMinimal {
		t.Fatalf("minimal: m=%v err=%v", m, err)
	}
	if _, err := ParseLoadOutputsMode("nope"); err == nil {
		t.Fatal("want error")
	}
}

func TestParseOutputMode(t *testing.T) {
	if m, err := ParseOutputMode("terse"); err != nil || m != OutputModeTerse {
		t.Fatal("terse")
	}
	if m, err := ParseOutputMode(""); err != nil || m != OutputModeTerse {
		t.Fatal("empty")
	}
	if m, err := ParseOutputMode("detailed"); err != nil || m != OutputModeDetailed {
		t.Fatal("detailed")
	}
	if _, err := ParseOutputMode("bogus"); err == nil {
		t.Fatal("want err")
	}
}
