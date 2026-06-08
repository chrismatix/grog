package hashing

import (
	"os"
	"path/filepath"
	"testing"

	"grog/internal/config"
	"grog/internal/dag"
	"grog/internal/label"
	"grog/internal/model"
)

func TestHashBytesAndString(t *testing.T) {
	if HashString("") == "" {
		t.Fatal("empty")
	}
	if HashBytes(nil) == "" {
		t.Fatal("nil bytes")
	}
	if HashString("a") == HashString("b") {
		t.Fatal("expected different")
	}
	if HashBytes([]byte("hello")) != HashString("hello") {
		t.Fatal("byte/string should equate when content is same")
	}
}

func TestHashStrings_OrderIndependent(t *testing.T) {
	a := HashStrings([]string{"a", "b", "c"})
	b := HashStrings([]string{"c", "b", "a"})
	if a != b {
		t.Fatal("expected order independent")
	}
}

func TestHashFile_ErrorOnMissing(t *testing.T) {
	if _, err := HashFile(filepath.Join(t.TempDir(), "nope")); err == nil {
		t.Fatal("expected err")
	}
}

func TestHashFiles_SkipsMissing(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	h, err := HashFiles(dir, []string{"a.txt", "missing.txt"})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if h == "" {
		t.Fatal("empty hash")
	}
}

func TestGetTargetChangeHash_WithAndWithoutInputs(t *testing.T) {
	dir := t.TempDir()
	prev := config.Global
	config.Global = config.WorkspaceConfig{WorkspaceRoot: dir, OS: "linux", Arch: "amd64"}
	t.Cleanup(func() { config.Global = prev })

	pkgDir := filepath.Join(dir, "pkg")
	if err := os.MkdirAll(pkgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkgDir, "src.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	target := model.Target{
		Label:   label.TL("pkg", "t"),
		Command: "echo",
		Inputs:  []string{"src.txt"},
	}
	h1, err := GetTargetChangeHash(target, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if h1 == "" {
		t.Fatal("empty")
	}

	target.Inputs = nil
	h2, err := GetTargetChangeHash(target, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if h2 == "" {
		t.Fatal("empty 2")
	}
	if h1 == h2 {
		t.Fatal("expected different with vs without inputs")
	}
}

func TestSetTargetChangeHash_DependencyMissingHash(t *testing.T) {
	dep := &model.Target{Label: label.TL("pkg", "dep")}
	tgt := &model.Target{
		Label:        label.TL("pkg", "t"),
		Dependencies: []label.TargetLabel{dep.Label},
	}
	g := dag.NewDirectedGraphFromTargets(dep, tgt)
	if err := g.AddEdge(dep, tgt); err != nil {
		t.Fatal(err)
	}

	th := NewTargetHasher(g)
	if err := th.SetTargetChangeHash(tgt); err == nil {
		t.Fatal("expected error since dep has no OutputHash")
	}
}
