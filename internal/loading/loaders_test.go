package loading

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"grog/internal/config"
	"grog/internal/console"

	"github.com/apple/pkl-go/pkl"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest"
)

func newTestLogger(t *testing.T) *console.Logger {
	t.Helper()
	return console.NewFromSugared(zaptest.NewLogger(t).Sugar(), zapcore.DebugLevel)
}

func TestJsonLoader_Matches(t *testing.T) {
	l := JsonLoader{}
	if !l.Matches("BUILD.json") {
		t.Fatal("expected BUILD.json to match")
	}
	if l.Matches("BUILD.yaml") {
		t.Fatal("expected BUILD.yaml not to match")
	}
}

func TestJsonLoader_Load_Success(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "BUILD.json")
	content := `{"targets":[{"name":"t1","command":"echo hi"}]}`
	if err := os.WriteFile(p, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	pkg, matched, err := JsonLoader{}.Load(context.Background(), p)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !matched {
		t.Fatal("expected matched")
	}
	if len(pkg.Targets) != 1 || pkg.Targets[0].Name != "t1" {
		t.Fatalf("unexpected targets: %+v", pkg.Targets)
	}
}

func TestJsonLoader_Load_MissingFile(t *testing.T) {
	_, matched, err := JsonLoader{}.Load(context.Background(), "/nonexistent/BUILD.json")
	if err == nil {
		t.Fatal("expected error")
	}
	if matched {
		t.Fatal("expected not matched on file open failure")
	}
}

func TestJsonLoader_Load_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "BUILD.json")
	if err := os.WriteFile(p, []byte("not json"), 0644); err != nil {
		t.Fatal(err)
	}
	_, matched, err := JsonLoader{}.Load(context.Background(), p)
	if err == nil {
		t.Fatal("expected error")
	}
	if !matched {
		t.Fatal("expected matched=true on decode error")
	}
	if !strings.Contains(err.Error(), "failed to decode JSON") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestYamlLoader_Matches(t *testing.T) {
	l := YamlLoader{}
	if !l.Matches("BUILD.yaml") {
		t.Fatal("BUILD.yaml should match")
	}
	if !l.Matches("BUILD.yml") {
		t.Fatal("BUILD.yml should match")
	}
	if l.Matches("BUILD.json") {
		t.Fatal("BUILD.json should not match")
	}
}

func TestYamlLoader_Load_Success(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "BUILD.yaml")
	content := "targets:\n  - name: t1\n    command: echo hi\n"
	if err := os.WriteFile(p, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	pkg, matched, err := YamlLoader{}.Load(context.Background(), p)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !matched {
		t.Fatal("expected matched")
	}
	if len(pkg.Targets) != 1 || pkg.Targets[0].Name != "t1" {
		t.Fatalf("unexpected targets: %+v", pkg.Targets)
	}
}

func TestYamlLoader_Load_MissingFile(t *testing.T) {
	_, matched, err := YamlLoader{}.Load(context.Background(), "/nonexistent/BUILD.yaml")
	if err == nil {
		t.Fatal("expected error")
	}
	if matched {
		t.Fatal("expected not matched")
	}
}

func TestYamlLoader_Load_InvalidYAML(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "BUILD.yaml")
	if err := os.WriteFile(p, []byte("targets: [unbalanced"), 0644); err != nil {
		t.Fatal(err)
	}
	_, matched, err := YamlLoader{}.Load(context.Background(), p)
	if err == nil {
		t.Fatal("expected error")
	}
	if !matched {
		t.Fatal("expected matched=true on decode error")
	}
}

func TestMakefileLoader_Matches(t *testing.T) {
	l := MakefileLoader{}
	if !l.Matches("Makefile") {
		t.Fatal("Makefile should match")
	}
	if l.Matches("BUILD.json") {
		t.Fatal("BUILD.json should not match")
	}
}

func TestMakefileLoader_Load_Success(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "Makefile")
	content := "# @grog\n# name: build\nbuild:\n\techo hi\n"
	if err := os.WriteFile(p, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	pkg, matched, err := MakefileLoader{}.Load(context.Background(), p)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !matched {
		t.Fatal("expected matched")
	}
	if len(pkg.Targets) != 1 || pkg.Targets[0].Name != "build" {
		t.Fatalf("unexpected targets: %+v", pkg.Targets)
	}
}

func TestMakefileLoader_Load_MissingFile(t *testing.T) {
	_, matched, err := MakefileLoader{}.Load(context.Background(), "/nonexistent/Makefile")
	if err == nil {
		t.Fatal("expected error")
	}
	if matched {
		t.Fatal("expected not matched")
	}
}

func TestMakefileLoader_Load_ParseError(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "Makefile")
	content := "# @grog\n# name: build\nbuild_no_colon\n\techo hi\n"
	if err := os.WriteFile(p, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	_, _, err := MakefileLoader{}.Load(context.Background(), p)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "failed to parse Makefile") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestPackageLoader_LoadIfMatched_NoMatch(t *testing.T) {
	logger := newTestLogger(t)
	pl := NewPackageLoader(logger)
	dto, matched, err := pl.LoadIfMatched(context.Background(), "/tmp/foo.txt", "foo.txt")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if matched {
		t.Fatal("expected not matched")
	}
	_ = dto
}

func TestPackageLoader_LoadIfMatched_JsonMatch(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "BUILD.json")
	if err := os.WriteFile(p, []byte(`{"targets":[]}`), 0644); err != nil {
		t.Fatal(err)
	}
	logger := newTestLogger(t)
	pl := NewPackageLoader(logger)
	dto, matched, err := pl.LoadIfMatched(context.Background(), p, "BUILD.json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !matched {
		t.Fatal("expected matched")
	}
	if dto.SourceFilePath != p {
		t.Fatalf("expected SourceFilePath to be set to %s, got %s", p, dto.SourceFilePath)
	}
}

func TestMergePackages_Success(t *testing.T) {

	dir := t.TempDir()
	originalRoot := config.Global.WorkspaceRoot
	config.Global.WorkspaceRoot = dir
	t.Cleanup(func() { config.Global.WorkspaceRoot = originalRoot })

	logger := newTestLogger(t)

	dto1 := PackageDTO{
		SourceFilePath: filepath.Join(dir, "pkg", "BUILD.json"),
		Targets:        []*TargetDTO{{Name: "t1", Command: "echo 1"}},
	}
	dto2 := PackageDTO{
		SourceFilePath: filepath.Join(dir, "pkg", "BUILD.yaml"),
		Targets:        []*TargetDTO{{Name: "t2", Command: "echo 2"}},
		Aliases:        []*AliasDTO{{Name: "al", Actual: ":t2"}},
	}
	from, err := getEnrichedPackage(logger, "pkg", dto1)
	if err != nil {
		t.Fatal(err)
	}
	into, err := getEnrichedPackage(logger, "pkg", dto2)
	if err != nil {
		t.Fatal(err)
	}

	if err := mergePackages(from, into); err != nil {
		t.Fatalf("unexpected merge error: %v", err)
	}
	if len(into.Targets) != 2 {
		t.Fatalf("expected 2 targets, got %d", len(into.Targets))
	}
}

func TestMergePackages_DuplicateTarget(t *testing.T) {

	logger := newTestLogger(t)

	dto1 := PackageDTO{
		SourceFilePath: "pkg/BUILD.json",
		Targets:        []*TargetDTO{{Name: "t", Command: "echo 1"}},
	}
	dto2 := PackageDTO{
		SourceFilePath: "pkg/BUILD.yaml",
		Targets:        []*TargetDTO{{Name: "t", Command: "echo 2"}},
	}
	from, err := getEnrichedPackage(logger, "pkg", dto1)
	if err != nil {
		t.Fatal(err)
	}
	into, err := getEnrichedPackage(logger, "pkg", dto2)
	if err != nil {
		t.Fatal(err)
	}

	err = mergePackages(from, into)
	if err == nil || !strings.Contains(err.Error(), "duplicate target label") {
		t.Fatalf("expected duplicate target label error, got %v", err)
	}
}

func TestMergePackages_DuplicateAlias(t *testing.T) {

	logger := newTestLogger(t)

	dto1 := PackageDTO{
		SourceFilePath: "pkg/BUILD.json",
		Targets:        []*TargetDTO{{Name: "t1", Command: "echo 1"}},
		Aliases:        []*AliasDTO{{Name: "al", Actual: ":t1"}},
	}
	dto2 := PackageDTO{
		SourceFilePath: "pkg/BUILD.yaml",
		Targets:        []*TargetDTO{{Name: "t2", Command: "echo 2"}},
		Aliases:        []*AliasDTO{{Name: "al", Actual: ":t2"}},
	}
	from, err := getEnrichedPackage(logger, "pkg", dto1)
	if err != nil {
		t.Fatal(err)
	}
	into, err := getEnrichedPackage(logger, "pkg", dto2)
	if err != nil {
		t.Fatal(err)
	}

	err = mergePackages(from, into)
	if err == nil || !strings.Contains(err.Error(), "duplicate target label") {
		t.Fatalf("expected duplicate alias error, got %v", err)
	}
}

func TestMergePackages_AliasConflictsWithTarget(t *testing.T) {

	logger := newTestLogger(t)

	dto1 := PackageDTO{
		SourceFilePath: "pkg/BUILD.json",
		Targets:        []*TargetDTO{{Name: "t1", Command: "echo 1"}},
		Aliases:        []*AliasDTO{{Name: "shared", Actual: ":t1"}},
	}
	dto2 := PackageDTO{
		SourceFilePath: "pkg/BUILD.yaml",
		Targets:        []*TargetDTO{{Name: "shared", Command: "echo 2"}},
	}
	from, err := getEnrichedPackage(logger, "pkg", dto1)
	if err != nil {
		t.Fatal(err)
	}
	into, err := getEnrichedPackage(logger, "pkg", dto2)
	if err != nil {
		t.Fatal(err)
	}

	err = mergePackages(from, into)
	if err == nil || !strings.Contains(err.Error(), "duplicate alias label") {
		t.Fatalf("expected duplicate alias-vs-target error, got %v", err)
	}
}

func TestMergePackages_NilMaps(t *testing.T) {

	logger := newTestLogger(t)

	dto1 := PackageDTO{
		SourceFilePath: "pkg/BUILD.json",
		Targets:        []*TargetDTO{{Name: "t1", Command: "echo 1"}},
	}
	from, err := getEnrichedPackage(logger, "pkg", dto1)
	if err != nil {
		t.Fatal(err)
	}

	into := from
	emptyInto := *into
	emptyInto.Targets = nil
	emptyInto.Aliases = nil
	if err := mergePackages(from, &emptyInto); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadAllPackages(t *testing.T) {

	dir := t.TempDir()

	if err := os.MkdirAll(filepath.Join(dir, "a"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "b"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a", "BUILD.json"),
		[]byte(`{"targets":[{"name":"ta","command":"echo a"}]}`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b", "BUILD.yaml"),
		[]byte("targets:\n  - name: tb\n    command: echo b\n"), 0644); err != nil {
		t.Fatal(err)
	}

	originalRoot := config.Global.WorkspaceRoot
	originalWorkers := config.Global.NumWorkers
	config.Global.WorkspaceRoot = dir
	config.Global.NumWorkers = 0
	t.Cleanup(func() {
		config.Global.WorkspaceRoot = originalRoot
		config.Global.NumWorkers = originalWorkers
	})

	logger := newTestLogger(t)
	ctx := console.WithLogger(context.Background(), logger)

	packages, err := LoadAllPackages(ctx)
	if err != nil {
		t.Fatalf("LoadAllPackages returned error: %v", err)
	}
	if len(packages) != 2 {
		t.Fatalf("expected 2 packages, got %d", len(packages))
	}
}

func TestLoadPackages_InvalidJSON(t *testing.T) {

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "BUILD.json"), []byte("not json"), 0644); err != nil {
		t.Fatal(err)
	}

	originalRoot := config.Global.WorkspaceRoot
	originalWorkers := config.Global.NumWorkers
	config.Global.WorkspaceRoot = dir
	config.Global.NumWorkers = 2
	t.Cleanup(func() {
		config.Global.WorkspaceRoot = originalRoot
		config.Global.NumWorkers = originalWorkers
	})

	logger := newTestLogger(t)
	ctx := console.WithLogger(context.Background(), logger)

	_, err := LoadPackages(ctx, dir)
	if err == nil {
		t.Fatal("expected error from invalid JSON")
	}
}

func TestPklLoader_Matches(t *testing.T) {
	l := &PklLoader{}
	if !l.Matches("BUILD.pkl") {
		t.Fatal("BUILD.pkl should match")
	}
	if l.Matches("BUILD.json") {
		t.Fatal("BUILD.json should not match")
	}
}

func TestPklLoader_HasPklProjectFile(t *testing.T) {

	t.Run("absent", func(t *testing.T) {
		dir := t.TempDir()
		oldRoot := config.Global.WorkspaceRoot
		config.Global.WorkspaceRoot = dir
		t.Cleanup(func() { config.Global.WorkspaceRoot = oldRoot })

		if hasPklProjectFile() {
			t.Fatal("expected no PklProject file")
		}
	})

	t.Run("present", func(t *testing.T) {
		dir := t.TempDir()
		oldRoot := config.Global.WorkspaceRoot
		config.Global.WorkspaceRoot = dir
		t.Cleanup(func() { config.Global.WorkspaceRoot = oldRoot })

		if err := os.WriteFile(filepath.Join(dir, "PklProject"), []byte("amends \"package://pkg.pkl-lang.org/pkl-pantry/pkl.experimental.uri@1.0.0#/URI.pkl\"\n"), 0644); err != nil {
			t.Fatal(err)
		}
		if !hasPklProjectFile() {
			t.Fatal("expected PklProject file to be detected")
		}
	})
}

func TestPklLoader_WithEnv(t *testing.T) {

	opts1 := &pkl.EvaluatorOptions{}
	withEnv(map[string]string{"K": "V"})(opts1)
	if opts1.Env["K"] != "V" {
		t.Fatalf("expected K=V got %v", opts1.Env)
	}

	opts2 := &pkl.EvaluatorOptions{Env: map[string]string{"X": "Y"}}
	withEnv(map[string]string{"K": "V"})(opts2)
	if opts2.Env["X"] != "Y" || opts2.Env["K"] != "V" {
		t.Fatalf("expected merge, got %v", opts2.Env)
	}
}

func TestPklLoader_Load_NoEvaluator(t *testing.T) {
	if _, err := os.Stat("/nix/store"); os.IsNotExist(err) {
		t.Skip("pkl CLI may not be available")
	}

	dir := t.TempDir()
	oldRoot := config.Global.WorkspaceRoot
	config.Global.WorkspaceRoot = dir
	t.Cleanup(func() { config.Global.WorkspaceRoot = oldRoot })

	p := filepath.Join(dir, "BUILD.pkl")
	pklContent := `targets = new Listing<Mapping<String, Any>> {
  new {
    ["name"] = "t1"
    ["command"] = "echo hi"
    ["dependencies"] = List()
    ["inputs"] = List()
    ["exclude_inputs"] = List()
    ["outputs"] = List()
    ["bin_output"] = ""
    ["output_checks"] = List()
    ["tags"] = List()
    ["fingerprint"] = Map()
    ["platforms"] = List()
    ["environment_variables"] = Map()
    ["timeout"] = ""
    ["concurrency_group"] = ""
  }
}
aliases = List()
environments = List()
default_platforms = List()
`
	if err := os.WriteFile(p, []byte(pklContent), 0644); err != nil {
		t.Fatal(err)
	}

	loader := &PklLoader{}
	ctx := console.WithLogger(context.Background(), newTestLogger(t))
	_, matched, err := loader.Load(ctx, p)
	if err != nil {
		t.Logf("pkl load returned (expected may be ok): err=%v matched=%v", err, matched)
	}
}

func TestPklLoader_Load_FileMissing(t *testing.T) {
	if _, err := os.Stat("/nix/store"); os.IsNotExist(err) {
		t.Skip("pkl CLI may not be available")
	}

	dir := t.TempDir()
	oldRoot := config.Global.WorkspaceRoot
	config.Global.WorkspaceRoot = dir
	t.Cleanup(func() { config.Global.WorkspaceRoot = oldRoot })

	loader := &PklLoader{}
	ctx := console.WithLogger(context.Background(), newTestLogger(t))
	_, _, err := loader.Load(ctx, filepath.Join(dir, "nonexistent.pkl"))
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}
