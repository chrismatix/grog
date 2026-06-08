package loading

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"grog/internal/config"
	"grog/internal/console"
)

func TestLoadScriptTarget_PathOutsideWorkspace(t *testing.T) {
	dir := t.TempDir()
	otherDir := t.TempDir()

	originalRoot := config.Global.WorkspaceRoot
	config.Global.WorkspaceRoot = dir
	t.Cleanup(func() { config.Global.WorkspaceRoot = originalRoot })

	script := writeTempScript(t, otherDir, "x.grog.sh", "# @grog\necho hi\n")
	logger := newTestLogger(t)
	_, err := LoadScriptTarget(context.Background(), logger, script)
	if err == nil {
		t.Fatal("expected error for path outside workspace")
	}
}

func TestLoadScriptTarget_MissingFile(t *testing.T) {
	dir := t.TempDir()
	originalRoot := config.Global.WorkspaceRoot
	config.Global.WorkspaceRoot = dir
	t.Cleanup(func() { config.Global.WorkspaceRoot = originalRoot })

	logger := newTestLogger(t)
	_, err := LoadScriptTarget(context.Background(), logger, filepath.Join(dir, "missing.grog.sh"))
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestScriptLoader_AnnotationError(t *testing.T) {
	dir := t.TempDir()
	script := writeTempScript(t, dir, "bad.grog.sh", "# @grog\n# name: x: extra invalid\nexit 0\n")
	loader := ScriptLoader{}
	_, _, err := loader.Load(context.Background(), script)
	if err == nil || !strings.Contains(err.Error(), "failed to parse annotation") {
		t.Fatalf("expected annotation parse error, got %v", err)
	}
}

func TestScriptLoader_FileMissing(t *testing.T) {
	_, _, err := ScriptLoader{}.Load(context.Background(), "/nonexistent/x.grog.sh")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestScriptLoader_Matches(t *testing.T) {
	l := ScriptLoader{}
	if !l.Matches("foo.grog.sh") {
		t.Fatal("expected .grog.sh to match")
	}
	if !l.Matches("foo.grog.py") {
		t.Fatal("expected .grog.py to match")
	}
	if l.Matches("foo.sh") {
		t.Fatal("expected .sh not to match")
	}
}

func TestPrependUnique(t *testing.T) {
	got := prependUnique([]string{"a", "b"}, "c")
	if len(got) != 3 || got[0] != "c" {
		t.Fatalf("unexpected: %v", got)
	}

	already := prependUnique([]string{"a", "b"}, "a")
	if len(already) != 2 || already[0] != "a" {
		t.Fatalf("unexpected: %v", already)
	}
}

func TestLoadScriptTarget_HappyPath_NoAnnotation(t *testing.T) {
	dir := t.TempDir()
	originalRoot := config.Global.WorkspaceRoot
	config.Global.WorkspaceRoot = dir
	t.Cleanup(func() { config.Global.WorkspaceRoot = originalRoot })

	script := writeTempScript(t, dir, "plain.grog.sh", "#!/usr/bin/env bash\necho ok\n")
	logger := console.NewFromSugared(newTestLogger(t).Desugar().Sugar(), 0)
	tgt, err := LoadScriptTarget(context.Background(), logger, script)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tgt.Label.Name != "plain.grog.sh" {
		t.Fatalf("expected default name to be script name, got %s", tgt.Label.Name)
	}
}

func TestScriptLoader_AnnotationWithEmptyLines(t *testing.T) {
	dir := t.TempDir()
	script := writeTempScript(t, dir, "x.grog.sh", `#!/usr/bin/env bash
# @grog

# name: cooltarget

echo hi
`)
	pkg, matched, err := ScriptLoader{}.Load(context.Background(), script)
	if err != nil {
		t.Fatal(err)
	}
	if !matched {
		t.Fatal("expected matched")
	}
	if len(pkg.Targets) != 1 || pkg.Targets[0].Name != "cooltarget" {
		t.Fatalf("unexpected: %+v", pkg.Targets)
	}
}

func TestScriptLoader_EmptyAnnotationBlockNoBody(t *testing.T) {
	dir := t.TempDir()
	script := writeTempScript(t, dir, "x.grog.sh", "# @grog\necho hi\n")
	pkg, matched, err := ScriptLoader{}.Load(context.Background(), script)
	if err != nil {
		t.Fatal(err)
	}
	if !matched {
		t.Fatal("expected matched")
	}
	if len(pkg.Targets) != 1 || pkg.Targets[0].Name != "x.grog.sh" {
		t.Fatalf("expected default name, got %+v", pkg.Targets)
	}
}

func TestScriptLoader_ScannerError(t *testing.T) {
	dir := t.TempDir()
	long := strings.Repeat("# ", 100000)
	script := writeTempScript(t, dir, "huge.grog.sh", "# @grog\n"+long+"\n")
	_, _, _ = ScriptLoader{}.Load(context.Background(), script)
}
