package lsp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDiagnosticsForStarlarkAcceptsGrogTarget(t *testing.T) {
	diagnostics := diagnosticsFor("file:///repo/BUILD.star", `target(name = "build", command = "go build ./...")`)
	if len(diagnostics) != 0 {
		t.Fatalf("expected no diagnostics, got %#v", diagnostics)
	}
}

func TestDiagnosticsForStarlarkReportsMissingTargetName(t *testing.T) {
	diagnostics := diagnosticsFor("file:///repo/BUILD.star", `target(command = "go build ./...")`)
	if len(diagnostics) == 0 {
		t.Fatalf("expected diagnostics")
	}
}

func TestDiagnosticsForStarlarkReportsDuplicateName(t *testing.T) {
	diagnostics := diagnosticsFor("file:///repo/BUILD.star", "target(name = \"build\")\nalias(name = \"build\", actual = \":other\")\n")
	if len(diagnostics) == 0 {
		t.Fatalf("expected duplicate name diagnostic")
	}
}

func TestDiagnosticsForStarlarkHighlightsRepeatedKeywordName(t *testing.T) {
	diagnostics := diagnosticsFor("file:///repo/BUILD.star", `target(name = "build", name = "again")`)
	for _, diagnostic := range diagnostics {
		if diagnostic.Range.Start.Character == 23 && diagnostic.Range.End.Character == 24 {
			t.Fatalf("did not expect single-character repeated keyword diagnostic: %#v", diagnostic)
		}
	}
	for _, diagnostic := range diagnostics {
		if diagnostic.Message == `duplicate keyword argument "name"` {
			if diagnostic.Range.Start.Character != 23 || diagnostic.Range.End.Character != 27 {
				t.Fatalf("expected repeated keyword range 23:27, got %#v", diagnostic.Range)
			}
			return
		}
	}
	t.Fatalf("expected duplicate keyword diagnostic, got %#v", diagnostics)
}

func TestDiagnosticsForYaml(t *testing.T) {
	diagnostics := diagnosticsFor("file:///repo/BUILD.yaml", "targets:\n  - name: build\n    command: go build ./...\n")
	if len(diagnostics) != 0 {
		t.Fatalf("expected no diagnostics, got %#v", diagnostics)
	}
}

func TestDiagnosticsForYamlReportsMissingName(t *testing.T) {
	diagnostics := diagnosticsFor("file:///repo/BUILD.yaml", "targets:\n  - command: go build ./...\n")
	if len(diagnostics) == 0 {
		t.Fatalf("expected missing name diagnostic")
	}
}

func TestDiagnosticsForYamlReportsDuplicateName(t *testing.T) {
	diagnostics := diagnosticsFor("file:///repo/BUILD.yaml", "targets:\n  - name: build\naliases:\n  - name: build\n    actual: :other\n")
	if len(diagnostics) == 0 {
		t.Fatalf("expected duplicate name diagnostic")
	}
}

func TestStarlarkCompletionDoesNotSuggestYamlTopLevelFields(t *testing.T) {
	server := &server{documents: map[string]string{"file:///repo/BUILD.star": ""}}
	items := server.completionItems("file:///repo/BUILD.star", position{})
	for _, item := range items {
		if item["label"] == "targets" {
			t.Fatalf("did not expect starlark completion to suggest yaml top-level field targets")
		}
	}
}

func TestStarlarkDependencyCompletionSuggestsLabels(t *testing.T) {
	tempDir := t.TempDir()
	buildPath := filepath.Join(tempDir, "BUILD.star")
	text := "target(name = \"build\")\ntarget(name = \"test\", dependencies = [\""
	if err := os.WriteFile(buildPath, []byte(text), 0644); err != nil {
		t.Fatalf("write build file: %v", err)
	}
	server := &server{documents: map[string]string{pathToURI(buildPath): text}}
	items := server.completionItems(pathToURI(buildPath), position{Line: 1, Character: 39})
	if !hasCompletionLabel(items, ":build") {
		t.Fatalf("expected :build completion item, got %#v", items)
	}
}

func TestStarlarkDependencyCompletionPrefersAbsoluteLabels(t *testing.T) {
	tempDir := t.TempDir()
	buildPath := filepath.Join(tempDir, "BUILD.star")
	text := "target(name = \"build\")\ntarget(name = \"test\", dependencies = [\""
	if err := os.WriteFile(buildPath, []byte(text), 0644); err != nil {
		t.Fatalf("write build file: %v", err)
	}
	server := &server{documents: map[string]string{pathToURI(buildPath): text}}
	items := server.completionItems(pathToURI(buildPath), position{Line: 1, Character: 39})
	if len(items) < 2 || items[0]["label"] != "//:build" || items[1]["label"] != "//:test" {
		t.Fatalf("expected absolute labels first, got %#v", items)
	}
}

func TestStarlarkDependencyCompletionKeepsLocalLabelsWhenPrefixStartsWithColon(t *testing.T) {
	tempDir := t.TempDir()
	buildPath := filepath.Join(tempDir, "BUILD.star")
	text := "target(name = \"build\")\ntarget(name = \"test\", dependencies = [\":"
	if err := os.WriteFile(buildPath, []byte(text), 0644); err != nil {
		t.Fatalf("write build file: %v", err)
	}
	server := &server{documents: map[string]string{pathToURI(buildPath): text}}
	items := server.completionItems(pathToURI(buildPath), position{Line: 1, Character: 40})
	if len(items) == 0 || items[0]["label"] != ":build" {
		t.Fatalf("expected local labels for colon prefix, got %#v", items)
	}
}

func TestStarlarkDependencyCompletionSkipsAlreadyListedLabels(t *testing.T) {
	tempDir := t.TempDir()
	buildPath := filepath.Join(tempDir, "BUILD.star")
	text := "target(name = \"build\")\ntarget(name = \"test\", dependencies = [\"//:build\", \""
	if err := os.WriteFile(buildPath, []byte(text), 0644); err != nil {
		t.Fatalf("write build file: %v", err)
	}
	server := &server{documents: map[string]string{pathToURI(buildPath): text}}
	items := server.completionItems(pathToURI(buildPath), position{Line: 1, Character: 51})
	if hasCompletionLabel(items, "//:build") {
		t.Fatalf("did not expect already listed dependency, got %#v", items)
	}
}

func TestStarlarkDependencyCompletionFindsMultilineTargets(t *testing.T) {
	tempDir := t.TempDir()
	buildPath := filepath.Join(tempDir, "BUILD.star")
	text := "target(\n  name = \"build\",\n)\ntarget(name = \"test\", dependencies = [\""
	if err := os.WriteFile(buildPath, []byte(text), 0644); err != nil {
		t.Fatalf("write build file: %v", err)
	}
	server := &server{documents: map[string]string{pathToURI(buildPath): text}}
	items := server.completionItems(pathToURI(buildPath), position{Line: 3, Character: 39})
	if !hasCompletionLabel(items, ":build") {
		t.Fatalf("expected :build completion item, got %#v", items)
	}
}

func TestPathCompletionPrefix(t *testing.T) {
	prefix := pathCompletionPrefix("target(inputs = [\"src/ma", position{Line: 0, Character: 24})
	if prefix != "src/ma" {
		t.Fatalf("prefix = %q", prefix)
	}
}

func TestOutputDirPathCompletionCompletesPathAfterDirPrefix(t *testing.T) {
	tempDir := t.TempDir()
	if err := os.Mkdir(filepath.Join(tempDir, "dist"), 0755); err != nil {
		t.Fatalf("create dist directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tempDir, "doc.txt"), []byte(""), 0644); err != nil {
		t.Fatalf("create doc file: %v", err)
	}
	items := outputPathCompletionItems(filepath.Join(tempDir, "BUILD.star"), `target(outputs = ["dir::d`, position{Line: 0, Character: 25})
	if !hasCompletionLabel(items, "dir::dist/") {
		t.Fatalf("expected dir::dist/ completion item, got %#v", items)
	}
	if hasCompletionLabel(items, "dir::doc.txt") {
		t.Fatalf("did not expect file completion for dir:: output, got %#v", items)
	}
	for _, item := range items {
		if item["label"] == "dir::dist/" && item["insertText"] != "ist/" {
			t.Fatalf("expected insertText ist/ for existing dir::d prefix, got %#v", item["insertText"])
		}
	}
}

func TestPathCompletionSkipsAlreadyListedPaths(t *testing.T) {
	tempDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tempDir, "main.go"), []byte(""), 0644); err != nil {
		t.Fatalf("create main file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tempDir, "more.go"), []byte(""), 0644); err != nil {
		t.Fatalf("create more file: %v", err)
	}
	items := pathCompletionItems(filepath.Join(tempDir, "BUILD.star"), `target(inputs = ["main.go", "m`, position{Line: 0, Character: 30})
	if hasCompletionLabel(items, "main.go") {
		t.Fatalf("did not expect already listed path, got %#v", items)
	}
	if !hasCompletionLabel(items, "more.go") {
		t.Fatalf("expected more.go path, got %#v", items)
	}
}

func TestDotPathCompletionFiltersHiddenFiles(t *testing.T) {
	items := pathCompletionItems("/repo/BUILD.star", `target(inputs = [".g`, position{Line: 0, Character: 21})
	for _, item := range items {
		label, _ := item["label"].(string)
		if label != "" && !strings.HasPrefix(label, ".g") {
			t.Fatalf("expected .g-prefixed label, got %q", label)
		}
	}
}

func TestStarlarkPathStringDoesNotSuggestTopLevelSymbols(t *testing.T) {
	server := &server{documents: map[string]string{"file:///repo/BUILD.star": `target(name = "x", inputs = [".`}}
	items := server.completionItems("file:///repo/BUILD.star", position{Line: 0, Character: 30})
	for _, item := range items {
		if item["label"] == "target" || item["label"] == "GROG_OS" {
			t.Fatalf("did not expect top-level completion inside path string")
		}
	}
}

func TestStarlarkTargetCompletionSuggestsTargetFieldsAfterPartialIdentifier(t *testing.T) {
	server := &server{documents: map[string]string{"file:///repo/BUILD.star": "target(\n  na"}}
	items := server.completionItems("file:///repo/BUILD.star", position{Line: 1, Character: 4})
	if !hasCompletionLabel(items, "name") {
		t.Fatalf("expected name completion, got %#v", items)
	}
	for _, item := range items {
		if item["label"] == "actual" || item["label"] == "alias" {
			t.Fatalf("did not expect target completion to suggest %s", item["label"])
		}
	}
}

func TestStarlarkTargetCompletionSuppressesTargetFieldsAtEmptyArgument(t *testing.T) {
	server := &server{documents: map[string]string{"file:///repo/BUILD.star": "target(\n  "}}
	items := server.completionItems("file:///repo/BUILD.star", position{Line: 1, Character: 2})
	if items != nil {
		t.Fatalf("expected no completions at empty target argument, got %#v", items)
	}
}

func TestStarlarkTargetCompletionSuggestsTargetFieldsAfterComma(t *testing.T) {
	server := &server{documents: map[string]string{"file:///repo/BUILD.star": `target(name = "x", )`}}
	items := server.completionItems("file:///repo/BUILD.star", position{Line: 0, Character: 19})
	if !hasCompletionLabel(items, "command") {
		t.Fatalf("expected command completion after comma, got %#v", items)
	}
}

func TestStarlarkTargetCompletionDoesNotSuggestPathsAfterPreviousOutputField(t *testing.T) {
	server := &server{documents: map[string]string{"file:///repo/BUILD.star": "target(outputs = [\"dist\"])\ntarget(\n  na"}}
	items := server.completionItems("file:///repo/BUILD.star", position{Line: 2, Character: 4})
	if !hasCompletionLabel(items, "name") {
		t.Fatalf("expected target parameter completions, got %#v", items)
	}
	for _, item := range items {
		if item["documentation"] == "file path" {
			t.Fatalf("did not expect path completion inside target parameter list, got %#v", item)
		}
	}
}

func hasCompletionLabel(items []map[string]any, label string) bool {
	for _, item := range items {
		if item["label"] == label {
			return true
		}
	}
	return false
}

func TestDefinitionForStarlarkTargetLabel(t *testing.T) {
	server := &server{documents: map[string]string{"file:///repo/BUILD.star": "target(name = \"build\", command = \"go build\")\ntarget(name = \"test\", dependencies = [\":build\"])\n"}}
	definition := server.definition("file:///repo/BUILD.star", position{Line: 1, Character: 39})
	if definition == nil {
		t.Fatalf("expected definition")
	}
}

func TestDefinitionForStarlarkFunction(t *testing.T) {
	server := &server{documents: map[string]string{"file:///repo/BUILD.star": "def deb_target(name):\n  target(name = name)\n\ndeb_target(\"pkg\")\n"}}
	definition := server.definition("file:///repo/BUILD.star", position{Line: 3, Character: 2})
	if definition == nil {
		t.Fatalf("expected definition")
	}
}

func TestLoadedSymbolParsing(t *testing.T) {
	modulePath, symbol, ok := starlarkLoadedSymbol("/repo/BUILD.star", `load("defs.star", "deb_target", alias_target = "real_target")`, "alias_target")
	if !ok {
		t.Fatalf("expected loaded symbol")
	}
	if modulePath != "/repo/defs.star" {
		t.Fatalf("modulePath = %q", modulePath)
	}
	if symbol != "real_target" {
		t.Fatalf("symbol = %q", symbol)
	}
}
