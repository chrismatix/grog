package lsp

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestServeLifecycle(t *testing.T) {
	input := framedMessage(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`) +
		framedMessage(`{"jsonrpc":"2.0","id":2,"method":"shutdown"}`) +
		framedMessage(`{"jsonrpc":"2.0","method":"exit"}`)
	var output bytes.Buffer

	if operationError := Serve(context.Background(), strings.NewReader(input), &output); operationError != nil {
		t.Fatalf("serve: %v", operationError)
	}
	if !strings.Contains(output.String(), `"positionEncoding":"utf-16"`) {
		t.Fatalf("initialize response does not advertise UTF-16 positions: %s", output.String())
	}
	if !strings.Contains(output.String(), `"id":2`) || !strings.Contains(output.String(), `"result":null`) {
		t.Fatalf("missing shutdown response: %s", output.String())
	}
}

func TestServeExitBeforeShutdownFails(t *testing.T) {
	input := framedMessage(`{"jsonrpc":"2.0","method":"exit"}`)
	if operationError := Serve(context.Background(), strings.NewReader(input), &bytes.Buffer{}); operationError == nil {
		t.Fatal("expected exit before shutdown to fail")
	}
}

func framedMessage(payload string) string {
	return fmt.Sprintf("Content-Length: %d\r\n\r\n%s", len(payload), payload)
}

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

func TestDiagnosticsForStarlarkIgnoresDeclarationsInsideMacros(t *testing.T) {
	text := "def package(name):\n  target(name = name)\n\ntarget(name = \"build\")\n"
	if diagnostics := diagnosticsFor("file:///repo/BUILD.star", text); len(diagnostics) != 0 {
		t.Fatalf("expected no diagnostics, got %#v", diagnostics)
	}
}

func TestDiagnosticsForCurrentStarlarkSchema(t *testing.T) {
	text := `resource(name = "database", up = "start", ready = "ready")
target(name = "release", binary_requires_push = True, dependencies = [":database"])
value = json.encode({"platform": GROG_PLATFORM, "root": GROG_WORKSPACE_ROOT, "hash": GROG_GIT_HASH})`
	if diagnostics := diagnosticsFor("file:///repo/BUILD.star", text); len(diagnostics) != 0 {
		t.Fatalf("expected no diagnostics, got %#v", diagnostics)
	}
}

func TestDiagnosticsForStarlarkValidatesListElements(t *testing.T) {
	diagnostics := diagnosticsFor("file:///repo/BUILD.star", `target(name = "build", dependencies = [1])`)
	if len(diagnostics) == 0 {
		t.Fatal("expected invalid dependency diagnostic")
	}
}

func TestDiagnosticsForStarlarkUsesLoaderSyntaxOptions(t *testing.T) {
	diagnostics := diagnosticsFor("file:///repo/BUILD.star", `values = {"one", "two"}`)
	if len(diagnostics) == 0 {
		t.Fatal("expected unsupported set diagnostic")
	}
}

func TestStarlarkDiagnosticsReadWorkspaceVariables(t *testing.T) {
	workspaceRoot := t.TempDir()
	configuration := "environment_variables_file = \"environment.env\"\n\n[environment_variables]\nINLINE_VALUE = \"inline\"\n"
	if operationError := os.WriteFile(filepath.Join(workspaceRoot, "grog.toml"), []byte(configuration), 0o644); operationError != nil {
		t.Fatalf("write grog.toml: %v", operationError)
	}
	if operationError := os.WriteFile(filepath.Join(workspaceRoot, "environment.env"), []byte("FILE_VALUE=from-file\n"), 0o644); operationError != nil {
		t.Fatalf("write environment file: %v", operationError)
	}
	text := `target(name = "build", command = INLINE_VALUE + FILE_VALUE)`
	if diagnostics := starlarkDiagnostics(filepath.Join(workspaceRoot, "BUILD.star"), text); len(diagnostics) != 0 {
		t.Fatalf("expected no diagnostics, got %#v", diagnostics)
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

func TestDiagnosticsForYamlResource(t *testing.T) {
	diagnostics := diagnosticsFor("file:///repo/BUILD.yaml", "resources:\n  - name: database\n    up: docker compose up\n")
	if len(diagnostics) != 0 {
		t.Fatalf("expected no diagnostics, got %#v", diagnostics)
	}
}

func TestDiagnosticsForYamlReportsUnknownFields(t *testing.T) {
	diagnostics := diagnosticsFor("file:///repo/BUILD.yaml", "targets:\n  - name: build\n    commmand: go build ./...\n")
	if len(diagnostics) == 0 {
		t.Fatal("expected unknown field diagnostic")
	}
	if diagnostics[0].Range.Start.Line != 2 {
		t.Fatalf("expected diagnostic on line 2, got %#v", diagnostics[0].Range)
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
	temporaryDirectory := t.TempDir()
	buildPath := filepath.Join(temporaryDirectory, "BUILD.star")
	text := "target(name = \"build\")\ntarget(name = \"test\", dependencies = [\""
	if operationError := os.WriteFile(buildPath, []byte(text), 0644); operationError != nil {
		t.Fatalf("write build file: %v", operationError)
	}
	server := &server{documents: map[string]string{pathToURI(buildPath): text}}
	items := server.completionItems(pathToURI(buildPath), position{Line: 1, Character: 39})
	if !hasCompletionLabel(items, ":build") {
		t.Fatalf("expected :build completion item, got %#v", items)
	}
}

func TestStarlarkDependencyCompletionPrefersAbsoluteLabels(t *testing.T) {
	temporaryDirectory := t.TempDir()
	buildPath := filepath.Join(temporaryDirectory, "BUILD.star")
	text := "target(name = \"build\")\ntarget(name = \"test\", dependencies = [\""
	if operationError := os.WriteFile(buildPath, []byte(text), 0644); operationError != nil {
		t.Fatalf("write build file: %v", operationError)
	}
	server := &server{documents: map[string]string{pathToURI(buildPath): text}}
	items := server.completionItems(pathToURI(buildPath), position{Line: 1, Character: 39})
	if len(items) < 2 || items[0]["label"] != "//:build" || items[1]["label"] != "//:test" {
		t.Fatalf("expected absolute labels first, got %#v", items)
	}
}

func TestStarlarkDependencyCompletionKeepsLocalLabelsWhenPrefixStartsWithColon(t *testing.T) {
	temporaryDirectory := t.TempDir()
	buildPath := filepath.Join(temporaryDirectory, "BUILD.star")
	text := "target(name = \"build\")\ntarget(name = \"test\", dependencies = [\":"
	if operationError := os.WriteFile(buildPath, []byte(text), 0644); operationError != nil {
		t.Fatalf("write build file: %v", operationError)
	}
	server := &server{documents: map[string]string{pathToURI(buildPath): text}}
	items := server.completionItems(pathToURI(buildPath), position{Line: 1, Character: 40})
	if len(items) == 0 || items[0]["label"] != ":build" {
		t.Fatalf("expected local labels for colon prefix, got %#v", items)
	}
}

func TestStarlarkDependencyCompletionSkipsAlreadyListedLabels(t *testing.T) {
	temporaryDirectory := t.TempDir()
	buildPath := filepath.Join(temporaryDirectory, "BUILD.star")
	text := "target(name = \"build\")\ntarget(name = \"test\", dependencies = [\"//:build\", \""
	if operationError := os.WriteFile(buildPath, []byte(text), 0644); operationError != nil {
		t.Fatalf("write build file: %v", operationError)
	}
	server := &server{documents: map[string]string{pathToURI(buildPath): text}}
	items := server.completionItems(pathToURI(buildPath), position{Line: 1, Character: 51})
	if hasCompletionLabel(items, "//:build") {
		t.Fatalf("did not expect already listed dependency, got %#v", items)
	}
}

func TestStarlarkDependencyCompletionFindsMultilineTargets(t *testing.T) {
	temporaryDirectory := t.TempDir()
	buildPath := filepath.Join(temporaryDirectory, "BUILD.star")
	text := "target(\n  name = \"build\",\n)\ntarget(name = \"test\", dependencies = [\""
	if operationError := os.WriteFile(buildPath, []byte(text), 0644); operationError != nil {
		t.Fatalf("write build file: %v", operationError)
	}
	server := &server{documents: map[string]string{pathToURI(buildPath): text}}
	items := server.completionItems(pathToURI(buildPath), position{Line: 3, Character: 39})
	if !hasCompletionLabel(items, ":build") {
		t.Fatalf("expected :build completion item, got %#v", items)
	}
}

func TestYamlDependencyCompletionInsideBlockSequence(t *testing.T) {
	temporaryDirectory := t.TempDir()
	buildPath := filepath.Join(temporaryDirectory, "BUILD.yaml")
	diskText := "targets:\n  - name: build\n  - name: test\n    dependencies: []\n"
	if operationError := os.WriteFile(buildPath, []byte(diskText), 0o644); operationError != nil {
		t.Fatalf("write build file: %v", operationError)
	}
	text := "targets:\n  - name: build\n  - name: test\n    dependencies:\n      - \":bu"
	server := &server{documents: map[string]string{pathToURI(buildPath): text}}
	items := server.completionItems(pathToURI(buildPath), position{Line: 4, Character: 12})
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
	temporaryDirectory := t.TempDir()
	if operationError := os.Mkdir(filepath.Join(temporaryDirectory, "dist"), 0755); operationError != nil {
		t.Fatalf("create dist directory: %v", operationError)
	}
	if operationError := os.WriteFile(filepath.Join(temporaryDirectory, "doc.txt"), []byte(""), 0644); operationError != nil {
		t.Fatalf("create doc file: %v", operationError)
	}
	items := outputPathCompletionItems(filepath.Join(temporaryDirectory, "BUILD.star"), `target(outputs = ["dir::d`, position{Line: 0, Character: 25})
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
		if item["label"] == "dir::dist/" {
			textEdit, isMap := item["textEdit"].(map[string]any)
			if !isMap || textEdit["newText"] != "dir::dist/" {
				t.Fatalf("expected text edit to replace the full path prefix, got %#v", item["textEdit"])
			}
		}
	}
}

func TestPathCompletionSkipsAlreadyListedPaths(t *testing.T) {
	temporaryDirectory := t.TempDir()
	if operationError := os.WriteFile(filepath.Join(temporaryDirectory, "main.go"), []byte(""), 0644); operationError != nil {
		t.Fatalf("create main file: %v", operationError)
	}
	if operationError := os.WriteFile(filepath.Join(temporaryDirectory, "more.go"), []byte(""), 0644); operationError != nil {
		t.Fatalf("create more file: %v", operationError)
	}
	items := pathCompletionItems(filepath.Join(temporaryDirectory, "BUILD.star"), `target(inputs = ["main.go", "m`, position{Line: 0, Character: 30})
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

func TestStarlarkCompletionIgnoresQuotesInComments(t *testing.T) {
	text := "# don't hide completions\ntarget(\n  na"
	server := &server{documents: map[string]string{"file:///repo/BUILD.star": text}}
	items := server.completionItems("file:///repo/BUILD.star", position{Line: 2, Character: 4})
	if !hasCompletionLabel(items, "name") {
		t.Fatalf("expected name completion, got %#v", items)
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
	modulePath, symbol, found := starlarkLoadedSymbol("/repo/BUILD.star", `load("defs.star", "deb_target", alias_target = "real_target")`, "alias_target")
	if !found {
		t.Fatalf("expected loaded symbol")
	}
	if modulePath != "/repo/defs.star" {
		t.Fatalf("modulePath = %q", modulePath)
	}
	if symbol != "real_target" {
		t.Fatalf("symbol = %q", symbol)
	}
}

func TestLoadedSymbolParsingMultilineAbsoluteLoad(t *testing.T) {
	workspaceRoot := t.TempDir()
	if operationError := os.WriteFile(filepath.Join(workspaceRoot, "grog.toml"), nil, 0o644); operationError != nil {
		t.Fatalf("write grog.toml: %v", operationError)
	}
	currentPath := filepath.Join(workspaceRoot, "package", "BUILD.star")
	modulePath, symbol, found := starlarkLoadedSymbol(currentPath, "load(\n  \"//rules.star\",\n  local_name = \"remote_name\",\n)\n", "local_name")
	if !found {
		t.Fatal("expected loaded symbol")
	}
	if modulePath != filepath.Join(workspaceRoot, "rules.star") {
		t.Fatalf("modulePath = %q", modulePath)
	}
	if symbol != "remote_name" {
		t.Fatalf("symbol = %q", symbol)
	}
}

func TestDocumentSymbolsIncludeDeclarationsAndMacros(t *testing.T) {
	text := "def package(name):\n  target(name = name)\n\ntarget(name = \"build\")\nresource(name = \"database\", up = \"start\")\n"
	symbols := symbolsFromText("BUILD.star", text)
	for _, expectedName := range []string{"package", "build", "database"} {
		if !hasSymbol(symbols, expectedName) {
			t.Fatalf("expected symbol %q, got %#v", expectedName, symbols)
		}
	}
}

func TestDocumentSymbolsSupportYamlAndStarlarkModules(t *testing.T) {
	if symbols := symbolsFromText("BUILD.yaml", "resources:\n  - name: database\n    up: start\n"); !hasSymbol(symbols, "database") {
		t.Fatalf("expected YAML resource symbol, got %#v", symbols)
	}
	if symbols := symbolsFromText("rules.bzl", "def package(name):\n  pass\n"); !hasSymbol(symbols, "package") {
		t.Fatalf("expected Starlark macro symbol, got %#v", symbols)
	}
}

func TestDefinitionForAbsoluteTargetLabel(t *testing.T) {
	workspaceRoot := t.TempDir()
	if operationError := os.WriteFile(filepath.Join(workspaceRoot, "grog.toml"), nil, 0o644); operationError != nil {
		t.Fatalf("write grog.toml: %v", operationError)
	}
	dependencyDirectory := filepath.Join(workspaceRoot, "dependency")
	if operationError := os.Mkdir(dependencyDirectory, 0o755); operationError != nil {
		t.Fatalf("create dependency directory: %v", operationError)
	}
	if operationError := os.WriteFile(filepath.Join(dependencyDirectory, "BUILD.yaml"), []byte("targets:\n  - name: compile\n"), 0o644); operationError != nil {
		t.Fatalf("write dependency build file: %v", operationError)
	}
	buildPath := filepath.Join(workspaceRoot, "package", "BUILD.star")
	text := `target(name = "build", dependencies = ["//dependency:compile"])`
	server := &server{documents: map[string]string{pathToURI(buildPath): text}}
	definition := server.definition(pathToURI(buildPath), position{Line: 0, Character: 52})
	location, isMap := definition.(map[string]any)
	if !isMap || location["uri"] != pathToURI(filepath.Join(dependencyDirectory, "BUILD.yaml")) {
		t.Fatalf("unexpected definition: %#v", definition)
	}
}

func TestDefinitionForYamlTargetLabel(t *testing.T) {
	temporaryDirectory := t.TempDir()
	buildPath := filepath.Join(temporaryDirectory, "BUILD.yaml")
	text := "targets:\n  - name: build\n  - name: test\n    dependencies: [\":build\"]\n"
	server := &server{documents: map[string]string{pathToURI(buildPath): text}}
	if definition := server.definition(pathToURI(buildPath), position{Line: 3, Character: 21}); definition == nil {
		t.Fatal("expected YAML label definition")
	}
}

func TestDefinitionForShorthandAbsoluteTargetLabel(t *testing.T) {
	workspaceRoot := t.TempDir()
	if operationError := os.WriteFile(filepath.Join(workspaceRoot, "grog.toml"), nil, 0o644); operationError != nil {
		t.Fatalf("write grog.toml: %v", operationError)
	}
	dependencyDirectory := filepath.Join(workspaceRoot, "dependency")
	if operationError := os.Mkdir(dependencyDirectory, 0o755); operationError != nil {
		t.Fatalf("create dependency directory: %v", operationError)
	}
	dependencyPath := filepath.Join(dependencyDirectory, "BUILD.star")
	if operationError := os.WriteFile(dependencyPath, []byte(`target(name = "dependency")`), 0o644); operationError != nil {
		t.Fatalf("write dependency build file: %v", operationError)
	}
	buildPath := filepath.Join(workspaceRoot, "BUILD.star")
	text := `target(name = "build", dependencies = ["//dependency"])`
	server := &server{documents: map[string]string{pathToURI(buildPath): text}}
	location, isLocation := server.definition(pathToURI(buildPath), position{Line: 0, Character: 47}).(map[string]any)
	if !isLocation || location["uri"] != pathToURI(dependencyPath) {
		t.Fatalf("unexpected definition: %#v", location)
	}
}

func TestWorkspaceLabelsUseOpenDocumentsAndEvaluateMacros(t *testing.T) {
	workspaceRoot := t.TempDir()
	if operationError := os.WriteFile(filepath.Join(workspaceRoot, "grog.toml"), nil, 0o644); operationError != nil {
		t.Fatalf("write grog.toml: %v", operationError)
	}
	rulesPath := filepath.Join(workspaceRoot, "rules.star")
	rulesText := "def package(name):\n  target(name = name)\n"
	if operationError := os.WriteFile(rulesPath, []byte(rulesText), 0o644); operationError != nil {
		t.Fatalf("write rules: %v", operationError)
	}
	buildPath := filepath.Join(workspaceRoot, "BUILD.star")
	if operationError := os.WriteFile(buildPath, []byte(`target(name = "saved")`), 0o644); operationError != nil {
		t.Fatalf("write build file: %v", operationError)
	}
	openText := "load(\"rules.star\", \"package\")\npackage(\"generated\")\ntarget(name = \"open\")\n"
	server := &server{documents: map[string]string{pathToURI(buildPath): openText, pathToURI(rulesPath): rulesText}}
	labels := server.collectWorkspaceLabels(workspaceRoot, workspaceRoot)
	for _, expectedLabel := range []string{"//:generated", "//:open"} {
		if !slices.Contains(labels, expectedLabel) {
			t.Fatalf("expected label %q, got %#v", expectedLabel, labels)
		}
	}
	if slices.Contains(labels, "//:saved") {
		t.Fatalf("did not expect stale saved label, got %#v", labels)
	}
	location, isLocation := server.labelDefinition(buildPath, ":generated").(map[string]any)
	if !isLocation || location["uri"] != pathToURI(rulesPath) {
		t.Fatalf("unexpected macro-generated definition: %#v", location)
	}
}

func TestPositionConversionUsesUTF16CodeUnits(t *testing.T) {
	text := "é😀target"
	targetOffset := strings.Index(text, "target")
	textPosition := positionForOffset(text, targetOffset)
	if textPosition.Character != 3 {
		t.Fatalf("character = %d, want 3", textPosition.Character)
	}
	if offset := byteOffset(text, textPosition); offset != targetOffset {
		t.Fatalf("offset = %d, want %d", offset, targetOffset)
	}
}

func hasSymbol(symbols []map[string]any, name string) bool {
	for _, symbol := range symbols {
		if symbol["name"] == name {
			return true
		}
	}
	return false
}
