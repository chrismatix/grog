package lsp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"grog/internal/loading"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"go.starlark.net/starlark"
	"go.starlark.net/syntax"
	"gopkg.in/yaml.v3"
)

// Serve runs the grog language server over an LSP stdio transport.
func Serve(context context.Context, reader io.Reader, writer io.Writer) error {
	server := &server{reader: bufio.NewReader(reader), writer: writer, documents: map[string]string{}}
	return server.run(context)
}

type server struct {
	reader    *bufio.Reader
	writer    io.Writer
	documents map[string]string
	shutdown  bool
}

type message struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
}

func (s *server) run(context context.Context) error {
	for {
		select {
		case <-context.Done():
			return context.Err()
		default:
		}
		request, err := s.readMessage()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		if err := s.handle(request); err != nil {
			return err
		}
	}
}

func (s *server) readMessage() (message, error) {
	var length int
	for {
		line, err := s.reader.ReadString('\n')
		if err != nil {
			return message{}, err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(parts[0]), "Content-Length") {
			length, err = strconv.Atoi(strings.TrimSpace(parts[1]))
			if err != nil {
				return message{}, err
			}
		}
	}
	if length == 0 {
		return message{}, fmt.Errorf("missing Content-Length")
	}
	payload := make([]byte, length)
	if _, err := io.ReadFull(s.reader, payload); err != nil {
		return message{}, err
	}
	var request message
	if err := json.Unmarshal(payload, &request); err != nil {
		return message{}, err
	}
	return request, nil
}

func (s *server) handle(request message) error {
	switch request.Method {
	case "initialize":
		return s.respond(request.ID, map[string]any{"capabilities": map[string]any{
			"textDocumentSync":       1,
			"hoverProvider":          true,
			"definitionProvider":     true,
			"documentSymbolProvider": true,
			"completionProvider": map[string]any{
				"triggerCharacters": []string{"(", ",", "=", "\"", "'", ":", "_", "/", "."},
			},
			"signatureHelpProvider": map[string]any{"triggerCharacters": []string{"(", ","}},
		}})
	case "shutdown":
		s.shutdown = true
		return s.respond(request.ID, nil)
	case "exit":
		return io.EOF
	case "initialized", "$/cancelRequest":
		return nil
	case "textDocument/didOpen":
		var params didOpenParams
		_ = json.Unmarshal(request.Params, &params)
		s.documents[params.TextDocument.URI] = params.TextDocument.Text
		return s.publishDiagnostics(params.TextDocument.URI)
	case "textDocument/didChange":
		var params didChangeParams
		_ = json.Unmarshal(request.Params, &params)
		if len(params.ContentChanges) > 0 {
			s.documents[params.TextDocument.URI] = params.ContentChanges[len(params.ContentChanges)-1].Text
		}
		return s.publishDiagnostics(params.TextDocument.URI)
	case "textDocument/didSave":
		var params textDocumentParams
		_ = json.Unmarshal(request.Params, &params)
		return s.publishDiagnostics(params.TextDocument.URI)
	case "textDocument/completion":
		var params positionedTextDocumentParams
		_ = json.Unmarshal(request.Params, &params)
		return s.respond(request.ID, s.completionItems(params.TextDocument.URI, params.Position))
	case "textDocument/hover":
		var params positionedTextDocumentParams
		_ = json.Unmarshal(request.Params, &params)
		return s.respond(request.ID, s.hover(params.TextDocument.URI, params.Position))
	case "textDocument/signatureHelp":
		return s.respond(request.ID, signatureHelp())
	case "textDocument/definition":
		var params positionedTextDocumentParams
		_ = json.Unmarshal(request.Params, &params)
		return s.respond(request.ID, s.definition(params.TextDocument.URI, params.Position))
	case "textDocument/documentSymbol":
		var params textDocumentParams
		_ = json.Unmarshal(request.Params, &params)
		return s.respond(request.ID, s.documentSymbols(params.TextDocument.URI))
	default:
		if len(request.ID) > 0 {
			return s.respond(request.ID, nil)
		}
		return nil
	}
}

func (s *server) respond(id json.RawMessage, result any) error {
	return s.write(map[string]any{"jsonrpc": "2.0", "id": json.RawMessage(id), "result": result})
}

func (s *server) notify(method string, params any) error {
	return s.write(map[string]any{"jsonrpc": "2.0", "method": method, "params": params})
}

func (s *server) write(value any) error {
	payload, err := json.Marshal(value)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(s.writer, "Content-Length: %d\r\n\r\n%s", len(payload), payload)
	return err
}

type textDocument struct {
	URI  string `json:"uri"`
	Text string `json:"text"`
}
type textDocumentIdentifier struct {
	URI string `json:"uri"`
}
type didOpenParams struct {
	TextDocument textDocument `json:"textDocument"`
}
type didChangeParams struct {
	TextDocument   textDocumentIdentifier `json:"textDocument"`
	ContentChanges []struct {
		Text string `json:"text"`
	} `json:"contentChanges"`
}
type textDocumentParams struct {
	TextDocument textDocumentIdentifier `json:"textDocument"`
}

type positionedTextDocumentParams struct {
	TextDocument textDocumentIdentifier `json:"textDocument"`
	Position     position               `json:"position"`
}

type diagnostic struct {
	Range    rangeValue `json:"range"`
	Severity int        `json:"severity"`
	Source   string     `json:"source"`
	Message  string     `json:"message"`
}
type rangeValue struct {
	Start position `json:"start"`
	End   position `json:"end"`
}
type position struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}

func (s *server) publishDiagnostics(uri string) error {
	text := s.documents[uri]
	diagnostics := diagnosticsFor(uri, text)
	return s.notify("textDocument/publishDiagnostics", map[string]any{"uri": uri, "diagnostics": diagnostics})
}

func diagnosticsFor(uri string, text string) []diagnostic {
	path := uriPath(uri)
	name := filepath.Base(path)
	if name == "BUILD.star" {
		return starlarkDiagnostics(path, text)
	}
	if name == "BUILD.yaml" || name == "BUILD.yml" {
		return yamlDiagnostics(text)
	}
	return nil
}

func starlarkDiagnostics(path string, text string) []diagnostic {
	if _, err := syntax.Parse(path, text, syntax.RetainComments); err != nil {
		return []diagnostic{diagnosticFromError(err)}
	}
	predeclared := starlark.StringDict{
		"target":      starlark.NewBuiltin("target", targetBuiltin),
		"alias":       starlark.NewBuiltin("alias", aliasBuiltin),
		"environment": starlark.NewBuiltin("environment", environmentBuiltin),
		"GROG_OS":     starlark.String(""), "GROG_ARCH": starlark.String(""), "GROG_PLATFORM": starlark.String(""), "GROG_ENV_FILE": starlark.String(""), "GROG_PLATFORM_TAGS": starlark.NewList(nil),
	}
	thread := &starlark.Thread{
		Name: path,
		Load: loadModule(path),
	}
	_, err := starlark.ExecFile(thread, path, text, predeclared)
	diagnostics := starlarkSemanticDiagnostics(text)
	if err != nil && !isRepeatedKeywordArgumentError(err) {
		diagnostics = append([]diagnostic{diagnosticFromError(err)}, diagnostics...)
	}
	return diagnostics
}

func isRepeatedKeywordArgumentError(err error) bool {
	return strings.Contains(err.Error(), "keyword argument") && strings.Contains(err.Error(), "is repeated")
}

func targetBuiltin(thread *starlark.Thread, function *starlark.Builtin, arguments starlark.Tuple, keywordArguments []starlark.Tuple) (starlark.Value, error) {
	var name string
	var command string
	var dependencies, inputs, excludeInputs, outputs, outputChecks, tags, platforms *starlark.List
	var fingerprint, environmentVariables, ociPush *starlark.Dict
	var binOutput, timeout, concurrencyGroup string
	if err := starlark.UnpackArgs("target", arguments, keywordArguments, "name", &name, "command?", &command, "dependencies?", &dependencies, "inputs?", &inputs, "exclude_inputs?", &excludeInputs, "outputs?", &outputs, "bin_output?", &binOutput, "output_checks?", &outputChecks, "tags?", &tags, "fingerprint?", &fingerprint, "platforms?", &platforms, "environment_variables?", &environmentVariables, "timeout?", &timeout, "concurrency_group?", &concurrencyGroup, "oci_push?", &ociPush); err != nil {
		return nil, err
	}
	return starlark.None, nil
}

func aliasBuiltin(thread *starlark.Thread, function *starlark.Builtin, arguments starlark.Tuple, keywordArguments []starlark.Tuple) (starlark.Value, error) {
	var name string
	var actual string
	if err := starlark.UnpackArgs("alias", arguments, keywordArguments, "name", &name, "actual", &actual); err != nil {
		return nil, err
	}
	return starlark.None, nil
}

func environmentBuiltin(thread *starlark.Thread, function *starlark.Builtin, arguments starlark.Tuple, keywordArguments []starlark.Tuple) (starlark.Value, error) {
	var name string
	var environmentType string
	var dependencies *starlark.List
	var ociImage string
	if err := starlark.UnpackArgs("environment", arguments, keywordArguments, "name", &name, "type", &environmentType, "dependencies?", &dependencies, "oci_image?", &ociImage); err != nil {
		return nil, err
	}
	return starlark.None, nil
}

func starlarkSemanticDiagnostics(text string) []diagnostic {
	declarations := starlarkNamedDeclarations(text)
	diagnostics := duplicateNameDiagnostics(declarations)
	diagnostics = append(diagnostics, duplicateKeywordArgumentDiagnostics(text)...)
	return diagnostics
}

type namedDeclaration struct {
	kind       string
	name       string
	rangeValue rangeValue
}

func starlarkNamedDeclarations(text string) []namedDeclaration {
	pattern := regexp.MustCompile(`(?s)\b(target|alias|environment)\s*\([^)]*?\bname\s*=\s*["']([^"']+)["']`)
	declarations := []namedDeclaration{}
	matches := pattern.FindAllStringSubmatchIndex(text, -1)
	for _, match := range matches {
		kind := text[match[2]:match[3]]
		name := text[match[4]:match[5]]
		start := positionForOffset(text, match[4])
		end := position{Line: start.Line, Character: start.Character + len(name)}
		declarations = append(declarations, namedDeclaration{kind: kind, name: name, rangeValue: rangeValue{Start: start, End: end}})
	}
	return declarations
}

func duplicateKeywordArgumentDiagnostics(text string) []diagnostic {
	callPattern := regexp.MustCompile(`(?s)\b(target|alias|environment)\s*\(([^)]*)\)`)
	keywordPattern := regexp.MustCompile(`\b([A-Za-z_]\w*)\s*=`)
	diagnostics := []diagnostic{}
	for _, callMatch := range callPattern.FindAllStringSubmatchIndex(text, -1) {
		argumentsStart := callMatch[4]
		arguments := text[callMatch[4]:callMatch[5]]
		seen := map[string]string{}
		for _, keywordMatch := range keywordPattern.FindAllStringSubmatchIndex(arguments, -1) {
			name := arguments[keywordMatch[2]:keywordMatch[3]]
			if _, ok := seen[name]; !ok {
				seen[name] = name
				continue
			}
			startOffset := argumentsStart + keywordMatch[2]
			start := positionForOffset(text, startOffset)
			end := position{Line: start.Line, Character: start.Character + len(name)}
			diagnostics = append(diagnostics, diagnostic{Range: rangeValue{Start: start, End: end}, Severity: 1, Source: "grog", Message: fmt.Sprintf("duplicate keyword argument %q", name)})
		}
	}
	return diagnostics
}

func duplicateNameDiagnostics(declarations []namedDeclaration) []diagnostic {
	seen := map[string]namedDeclaration{}
	diagnostics := []diagnostic{}
	for _, declaration := range declarations {
		previous, ok := seen[declaration.name]
		if !ok {
			seen[declaration.name] = declaration
			continue
		}
		diagnostics = append(diagnostics, diagnostic{Range: declaration.rangeValue, Severity: 1, Source: "grog", Message: fmt.Sprintf("duplicate declaration name %q; first declared as %s", declaration.name, previous.kind)})
	}
	return diagnostics
}

func yamlSemanticDiagnostics(root *yaml.Node) []diagnostic {
	if root.Kind == yaml.DocumentNode && len(root.Content) > 0 {
		root = root.Content[0]
	}
	if root.Kind != yaml.MappingNode {
		return nil
	}
	diagnostics := []diagnostic{}
	declarations := []namedDeclaration{}
	for index := 0; index+1 < len(root.Content); index += 2 {
		key := root.Content[index]
		value := root.Content[index+1]
		switch key.Value {
		case "targets", "aliases", "environments":
			kind := strings.TrimSuffix(key.Value, "s")
			if value.Kind != yaml.SequenceNode {
				diagnostics = append(diagnostics, yamlNodeDiagnostic(value, fmt.Sprintf("%s must be a list", key.Value)))
				continue
			}
			for _, item := range value.Content {
				nameNode := yamlMappingValue(item, "name")
				if nameNode == nil || nameNode.Value == "" {
					diagnostics = append(diagnostics, yamlNodeDiagnostic(item, fmt.Sprintf("%s requires name", kind)))
					continue
				}
				declarations = append(declarations, namedDeclaration{kind: kind, name: nameNode.Value, rangeValue: yamlNodeRange(nameNode)})
			}
		}
	}
	diagnostics = append(diagnostics, duplicateNameDiagnostics(declarations)...)
	return diagnostics
}

func yamlMappingValue(node *yaml.Node, key string) *yaml.Node {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}
	for index := 0; index+1 < len(node.Content); index += 2 {
		if node.Content[index].Value == key {
			return node.Content[index+1]
		}
	}
	return nil
}

func yamlNodeDiagnostic(node *yaml.Node, message string) diagnostic {
	return diagnostic{Range: yamlNodeRange(node), Severity: 1, Source: "grog", Message: message}
}

func yamlNodeRange(node *yaml.Node) rangeValue {
	line := 0
	character := 0
	endCharacter := 1
	if node != nil {
		line = node.Line - 1
		character = node.Column - 1
		endCharacter = character + len(node.Value)
		if endCharacter <= character {
			endCharacter = character + 1
		}
	}
	if line < 0 {
		line = 0
	}
	if character < 0 {
		character = 0
	}
	return rangeValue{Start: position{Line: line, Character: character}, End: position{Line: line, Character: endCharacter}}
}

func loadModule(currentPath string) func(thread *starlark.Thread, module string) (starlark.StringDict, error) {
	loaded := map[string]starlark.StringDict{}
	var load func(thread *starlark.Thread, module string) (starlark.StringDict, error)
	load = func(thread *starlark.Thread, module string) (starlark.StringDict, error) {
		modulePath := resolveStarlarkModulePath(currentPath, module)
		if globals, ok := loaded[modulePath]; ok {
			return globals, nil
		}
		source, err := os.ReadFile(modulePath)
		if err != nil {
			return nil, fmt.Errorf("load %q: %w", module, err)
		}
		predeclared := starlark.StringDict{
			"target":      starlark.NewBuiltin("target", targetBuiltin),
			"alias":       starlark.NewBuiltin("alias", aliasBuiltin),
			"environment": starlark.NewBuiltin("environment", environmentBuiltin),
			"GROG_OS":     starlark.String(""), "GROG_ARCH": starlark.String(""), "GROG_PLATFORM": starlark.String(""), "GROG_ENV_FILE": starlark.String(""), "GROG_PLATFORM_TAGS": starlark.NewList(nil),
		}
		moduleThread := &starlark.Thread{Name: modulePath, Load: load}
		globals, err := starlark.ExecFile(moduleThread, modulePath, source, predeclared)
		if err != nil {
			return nil, err
		}
		loaded[modulePath] = globals
		return globals, nil
	}
	return load
}

func yamlDiagnostics(text string) []diagnostic {
	var packageDTO loading.PackageDTO
	if err := yaml.Unmarshal([]byte(text), &packageDTO); err != nil {
		return []diagnostic{diagnosticFromError(err)}
	}
	var root yaml.Node
	if err := yaml.Unmarshal([]byte(text), &root); err != nil {
		return []diagnostic{diagnosticFromError(err)}
	}
	return yamlSemanticDiagnostics(&root)
}

func diagnosticFromError(err error) diagnostic {
	line, character := 0, 0
	var syntaxError syntax.Error
	if errors.As(err, &syntaxError) {
		line = int(syntaxError.Pos.Line) - 1
		character = int(syntaxError.Pos.Col) - 1
	} else {
		re := regexp.MustCompile(`:(\d+):(\d+)`)
		matches := re.FindStringSubmatch(err.Error())
		if len(matches) == 3 {
			line, _ = strconv.Atoi(matches[1])
			character, _ = strconv.Atoi(matches[2])
			line--
			character--
		}
	}
	if line < 0 {
		line = 0
	}
	if character < 0 {
		character = 0
	}
	return diagnostic{Range: rangeValue{Start: position{Line: line, Character: character}, End: position{Line: line, Character: character + 1}}, Severity: 1, Source: "grog", Message: err.Error()}
}

func (s *server) completionItems(uri string, textPosition position) []map[string]any {
	path := uriPath(uri)
	text := s.documentText(uri)
	name := filepath.Base(path)
	if name == "BUILD.star" {
		return s.starlarkCompletionItems(uri, text, textPosition)
	}
	if field := yamlFieldAt(text, textPosition); field == "dependencies" {
		return labelCompletionItems(path, text, textPosition)
	} else if field == "inputs" || field == "exclude_inputs" || field == "bin_output" {
		return pathCompletionItems(path, text, textPosition)
	} else if field == "outputs" {
		return outputPathCompletionItems(path, text, textPosition)
	}
	return completionItemsFor([]string{"targets", "aliases", "environments", "default_platforms", "name", "command", "dependencies", "inputs", "exclude_inputs", "outputs", "bin_output", "output_checks", "tags", "fingerprint", "platforms", "environment_variables", "timeout", "concurrency_group", "oci_push", "actual", "type", "oci_image"}, 5)
}

func (s *server) documentText(uri string) string {
	if text, ok := s.documents[uri]; ok {
		return text
	}
	text, err := os.ReadFile(uriPath(uri))
	if err != nil {
		return ""
	}
	return string(text)
}

func (s *server) starlarkCompletionItems(uri string, text string, textPosition position) []map[string]any {
	path := uriPath(uri)
	field := starlarkFieldAt(text, textPosition)
	if inStringAt(text, textPosition) {
		if field == "dependencies" {
			return labelCompletionItems(path, text, textPosition)
		}
		if field == "inputs" || field == "exclude_inputs" || field == "bin_output" {
			return pathCompletionItems(path, text, textPosition)
		}
		if field == "outputs" {
			return outputPathCompletionItems(path, text, textPosition)
		}
		return nil
	}
	callName := enclosingStarlarkCall(text, textPosition)
	if callName != "" && !shouldSuggestStarlarkCallParameters(text, textPosition) {
		return nil
	}
	switch callName {
	case "target":
		return completionItemsFor([]string{"name", "command", "dependencies", "inputs", "exclude_inputs", "outputs", "bin_output", "output_checks", "tags", "fingerprint", "platforms", "environment_variables", "timeout", "concurrency_group", "oci_push"}, 5)
	case "alias":
		return completionItemsFor([]string{"name", "actual"}, 5)
	case "environment":
		return completionItemsFor([]string{"name", "type", "dependencies", "oci_image"}, 5)
	}
	items := completionItemsFor([]string{"target", "alias", "environment", "load"}, 3)
	items = append(items, completionItemsFor([]string{"GROG_OS", "GROG_ARCH", "GROG_PLATFORM", "GROG_PLATFORM_TAGS", "GROG_ENV_FILE"}, 6)...)
	return items
}

func labelCompletionItems(currentPath string, text string, textPosition position) []map[string]any {
	prefix := pathCompletionPrefix(text, textPosition)
	workspaceRoot := findWorkspaceRoot(filepath.Dir(currentPath))
	labels := collectWorkspaceLabels(workspaceRoot, filepath.Dir(currentPath))
	alreadyListed := stringListValuesAt(text, textPosition)
	items := []map[string]any{}
	for _, label := range preferredDependencyLabels(labels, prefix) {
		if prefix != "" && !strings.HasPrefix(label, prefix) || alreadyListed[label] {
			continue
		}
		items = append(items, map[string]any{"label": label, "kind": 12, "insertText": label, "sortText": fmt.Sprintf("%03d_%s", len(items), label), "documentation": "grog target label"})
		if len(items) >= 10 {
			break
		}
	}
	return items
}

func preferredDependencyLabels(labels []string, prefix string) []string {
	if strings.HasPrefix(prefix, ":") {
		return labels
	}
	preferred := make([]string, 0, len(labels))
	deferred := make([]string, 0, len(labels))
	for _, label := range labels {
		if strings.HasPrefix(label, "//") {
			preferred = append(preferred, label)
		} else {
			deferred = append(deferred, label)
		}
	}
	return append(preferred, deferred...)
}

func outputPathCompletionItems(currentPath string, text string, textPosition position) []map[string]any {
	prefix := pathCompletionPrefix(text, textPosition)
	for _, outputTypePrefix := range []string{"dir::"} {
		if strings.HasPrefix(prefix, outputTypePrefix) {
			return pathCompletionItemsWithPrefix(currentPath, strings.TrimPrefix(prefix, outputTypePrefix), outputTypePrefix, true, stringListValuesAt(text, textPosition))
		}
	}
	return pathCompletionItemsWithPrefix(currentPath, prefix, "", false, stringListValuesAt(text, textPosition))
}

func pathCompletionItems(currentPath string, text string, textPosition position) []map[string]any {
	return pathCompletionItemsWithPrefix(currentPath, pathCompletionPrefix(text, textPosition), "", false, stringListValuesAt(text, textPosition))
}

func pathCompletionItemsWithPrefix(currentPath string, prefix string, labelBasePrefix string, directoriesOnly bool, alreadyListed map[string]bool) []map[string]any {
	directory := filepath.Dir(currentPath)
	entryDirectory := directory
	entryPrefix := prefix
	labelPrefix := labelBasePrefix
	if prefix == "./" {
		entryPrefix = ""
		labelPrefix += "./"
	} else if slash := strings.LastIndex(prefix, "/"); slash >= 0 {
		entryDirectory = filepath.Join(directory, prefix[:slash+1])
		entryPrefix = prefix[slash+1:]
		labelPrefix += prefix[:slash+1]
	}
	entries, err := os.ReadDir(entryDirectory)
	if err != nil {
		return nil
	}
	items := []map[string]any{}
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasPrefix(name, entryPrefix) || directoriesOnly && !entry.IsDir() {
			continue
		}
		label := labelPrefix + name
		if entry.IsDir() {
			label += "/"
		}
		if alreadyListed[label] || alreadyListed[strings.TrimSuffix(label, "/")] {
			continue
		}
		insertText := label
		if prefix != "" && strings.HasPrefix(label, labelBasePrefix+prefix) {
			insertText = strings.TrimPrefix(label, labelBasePrefix+prefix)
		} else if strings.HasPrefix(name, entryPrefix) {
			insertText = strings.TrimPrefix(name, entryPrefix)
			if entry.IsDir() {
				insertText += "/"
			}
		}
		items = append(items, map[string]any{"label": label, "kind": 17, "insertText": insertText, "sortText": fmt.Sprintf("%03d_%s", len(items), label), "documentation": "file path"})
		if len(items) >= 10 {
			break
		}
	}
	return items
}

func pathCompletionPrefix(text string, textPosition position) string {
	lines := strings.Split(text, "\n")
	if textPosition.Line < 0 || textPosition.Line >= len(lines) {
		return ""
	}
	line := lines[textPosition.Line]
	if textPosition.Character < 0 || textPosition.Character > len(line) {
		return ""
	}
	prefix := line[:textPosition.Character]
	quote := strings.LastIndexAny(prefix, "\"'")
	if quote < 0 {
		return ""
	}
	return prefix[quote+1:]
}

func stringListValuesAt(text string, textPosition position) map[string]bool {
	values := map[string]bool{}
	offset := byteOffset(text, textPosition)
	if offset < 0 || offset > len(text) {
		return values
	}
	start := strings.LastIndex(text[:offset], "[")
	if start < 0 {
		return values
	}
	end := len(text)
	if endRelative := strings.Index(text[offset:], "]"); endRelative >= 0 {
		end = offset + endRelative
	}
	listText := text[start:end]
	stringPattern := regexp.MustCompile(`["']([^"']+)["']`)
	for _, match := range stringPattern.FindAllStringSubmatchIndex(listText, -1) {
		absoluteStart := start + match[0]
		absoluteEnd := start + match[1]
		if absoluteStart <= offset && offset <= absoluteEnd {
			continue
		}
		values[listText[match[2]:match[3]]] = true
	}
	return values
}

func completionItemsFor(names []string, kind int) []map[string]any {
	items := []map[string]any{}
	for index, itemName := range names {
		insertText := itemName
		if kind == 3 {
			insertText = itemName + "("
		}
		items = append(items, map[string]any{"label": itemName, "kind": kind, "insertText": insertText, "sortText": fmt.Sprintf("%03d_%s", index, itemName), "documentation": docs[itemName]})
	}
	return items
}

var docs = map[string]string{
	"target":                "Declare a grog build/test target.",
	"alias":                 "Declare an alias to another grog target.",
	"environment":           "Declare an execution environment.",
	"load":                  "Load symbols from another Starlark file using grog's local load resolution. No Bazel repositories are fetched.",
	"name":                  "The declaration name.",
	"command":               "Shell command run by a target.",
	"dependencies":          "Target labels this item depends on, such as :build or //pkg:test.",
	"inputs":                "Input files or globs used to fingerprint a target.",
	"exclude_inputs":        "Input globs to exclude from fingerprinting.",
	"outputs":               "Output paths produced by a target.",
	"bin_output":            "Executable output path produced by a target.",
	"output_checks":         "Checks that validate produced outputs.",
	"tags":                  "Tags used for filtering targets.",
	"fingerprint":           "Additional key/value fingerprint material.",
	"platforms":             "Platform selectors that constrain where this declaration applies.",
	"environment_variables": "Environment variables for the target.",
	"timeout":               "Target timeout duration.",
	"concurrency_group":     "Concurrency group used to serialize related targets.",
	"oci_push":              "OCI push destinations for declared OCI outputs.",
	"actual":                "Target label referenced by an alias.",
	"type":                  "Environment type.",
	"oci_image":             "OCI image used by an environment.",
	"targets":               "YAML list of grog targets.",
	"aliases":               "YAML list of grog aliases.",
	"environments":          "YAML list of grog environments.",
	"default_platforms":     "Default platform selectors for this package.",
}

func (s *server) hover(uri string, textPosition position) any {
	word := wordAt(s.documents[uri], textPosition)
	if word == "" {
		return nil
	}
	documentation, ok := docs[word]
	if !ok {
		return nil
	}
	return map[string]any{"contents": map[string]any{"kind": "markdown", "value": "**" + word + "**\n\n" + documentation}}
}
func starlarkFieldAt(text string, textPosition position) string {
	offset := byteOffset(text, textPosition)
	if offset < 0 || offset > len(text) {
		return ""
	}
	prefix := text[:offset]
	equals := strings.LastIndex(prefix, "=")
	if equals < 0 {
		return ""
	}
	start := equals
	for start > 0 && unicodeSpace(prefix[start-1]) {
		start--
	}
	end := start
	for start > 0 && isWordCharacter(prefix[start-1]) {
		start--
	}
	return prefix[start:end]
}

func unicodeSpace(character byte) bool {
	return character == ' ' || character == '\t' || character == '\n' || character == '\r'
}

func inStringAt(text string, textPosition position) bool {
	offset := byteOffset(text, textPosition)
	if offset < 0 || offset > len(text) {
		return false
	}
	inString := byte(0)
	for index := 0; index < offset; index++ {
		character := text[index]
		if inString != 0 {
			if character == inString && (index == 0 || text[index-1] != '\\') {
				inString = 0
			}
			continue
		}
		if character == '\'' || character == '"' {
			inString = character
		}
	}
	return inString != 0
}

func yamlFieldAt(text string, textPosition position) string {
	lines := strings.Split(text, "\n")
	for lineNumber := textPosition.Line; lineNumber >= 0 && lineNumber < len(lines); lineNumber-- {
		line := strings.TrimSpace(lines[lineNumber])
		if strings.HasSuffix(line, ":") {
			return strings.TrimSuffix(line, ":")
		}
		if strings.Contains(line, ":") && lineNumber == textPosition.Line {
			return strings.TrimSpace(strings.SplitN(line, ":", 2)[0])
		}
	}
	return ""
}

func shouldSuggestStarlarkCallParameters(text string, textPosition position) bool {
	offset := byteOffset(text, textPosition)
	if offset < 0 || offset > len(text) {
		return false
	}
	prefix := text[:offset]
	index := len(prefix) - 1
	for index >= 0 && unicodeSpace(prefix[index]) {
		index--
	}
	if index >= 0 && prefix[index] == ',' {
		return true
	}
	return len(prefix) > 0 && isWordCharacter(prefix[len(prefix)-1])
}

func enclosingStarlarkCall(text string, textPosition position) string {
	offset := byteOffset(text, textPosition)
	if offset < 0 || offset > len(text) {
		return ""
	}
	prefix := text[:offset]
	depth := 0
	inString := byte(0)
	for index := len(prefix) - 1; index >= 0; index-- {
		character := prefix[index]
		if inString != 0 {
			if character == inString && (index == 0 || prefix[index-1] != '\\') {
				inString = 0
			}
			continue
		}
		if character == '\'' || character == '"' {
			inString = character
			continue
		}
		switch character {
		case ')':
			depth++
		case '(':
			if depth > 0 {
				depth--
				continue
			}
			end := index
			start := end
			for start > 0 && isWordCharacter(prefix[start-1]) {
				start--
			}
			name := prefix[start:end]
			if name == "target" || name == "alias" || name == "environment" {
				return name
			}
			return ""
		}
	}
	return ""
}

func positionForOffset(text string, targetOffset int) position {
	line := 0
	character := 0
	if targetOffset < 0 {
		return position{}
	}
	for offset := 0; offset < len(text) && offset < targetOffset; offset++ {
		if text[offset] == '\n' {
			line++
			character = 0
			continue
		}
		character++
	}
	return position{Line: line, Character: character}
}

func byteOffset(text string, textPosition position) int {
	if textPosition.Line < 0 || textPosition.Character < 0 {
		return -1
	}
	line := 0
	character := 0
	for offset := 0; offset < len(text); offset++ {
		if line == textPosition.Line && character == textPosition.Character {
			return offset
		}
		if text[offset] == '\n' {
			line++
			character = 0
			continue
		}
		character++
	}
	if line == textPosition.Line && character == textPosition.Character {
		return len(text)
	}
	return -1
}

func wordAt(text string, textPosition position) string {
	lines := strings.Split(text, "\n")
	if textPosition.Line < 0 || textPosition.Line >= len(lines) {
		return ""
	}
	line := lines[textPosition.Line]
	if textPosition.Character < 0 || textPosition.Character > len(line) {
		return ""
	}
	start := textPosition.Character
	for start > 0 && isWordCharacter(line[start-1]) {
		start--
	}
	end := textPosition.Character
	for end < len(line) && isWordCharacter(line[end]) {
		end++
	}
	if start == end {
		return ""
	}
	return line[start:end]
}

func isWordCharacter(character byte) bool {
	return character == '_' || character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || character >= '0' && character <= '9'
}

func signatureHelp() map[string]any {
	return map[string]any{"signatures": []map[string]any{{"label": "target(name, command, dependencies, inputs, outputs, tags, ...)"}}, "activeSignature": 0, "activeParameter": 0}
}
func (s *server) definition(uri string, textPosition position) any {
	path := uriPath(uri)
	if filepath.Base(path) != "BUILD.star" {
		return nil
	}
	text := s.documents[uri]
	label := labelAt(text, textPosition)
	if label != "" {
		targetName := label
		if strings.HasPrefix(targetName, "//") {
			colon := strings.LastIndex(targetName, ":")
			if colon < 0 {
				return nil
			}
			targetName = targetName[colon+1:]
		} else {
			targetName = strings.TrimPrefix(targetName, ":")
		}
		definitionRange, ok := starlarkDeclarationRange(text, targetName)
		if !ok {
			return nil
		}
		return map[string]any{"uri": uri, "range": definitionRange}
	}
	word := wordAt(text, textPosition)
	if word == "" {
		return nil
	}
	definitionRange, ok := starlarkIdentifierDefinitionRange(text, word)
	if ok {
		return map[string]any{"uri": uri, "range": definitionRange}
	}
	modulePath, moduleSymbol, ok := starlarkLoadedSymbol(path, text, word)
	if !ok {
		return nil
	}
	moduleTextBytes, err := os.ReadFile(modulePath)
	if err != nil {
		return nil
	}
	definitionRange, ok = starlarkIdentifierDefinitionRange(string(moduleTextBytes), moduleSymbol)
	if !ok {
		return nil
	}
	return map[string]any{"uri": pathToURI(modulePath), "range": definitionRange}
}

func labelAt(text string, textPosition position) string {
	lines := strings.Split(text, "\n")
	if textPosition.Line < 0 || textPosition.Line >= len(lines) {
		return ""
	}
	line := lines[textPosition.Line]
	if textPosition.Character < 0 || textPosition.Character > len(line) {
		return ""
	}
	start := textPosition.Character
	for start > 0 && isLabelCharacter(line[start-1]) {
		start--
	}
	end := textPosition.Character
	for end < len(line) && isLabelCharacter(line[end]) {
		end++
	}
	label := line[start:end]
	if strings.HasPrefix(label, ":") || strings.HasPrefix(label, "//") {
		return label
	}
	return ""
}

func isLabelCharacter(character byte) bool {
	return isWordCharacter(character) || character == ':' || character == '/' || character == '-' || character == '.'
}

func findWorkspaceRoot(directory string) string {
	originalDirectory := directory
	for {
		if _, err := os.Stat(filepath.Join(directory, "grog.toml")); err == nil {
			return directory
		}
		parent := filepath.Dir(directory)
		if parent == directory {
			return originalDirectory
		}
		directory = parent
	}
}

func collectWorkspaceLabels(workspaceRoot string, currentDirectory string) []string {
	labels := []string{}
	_ = filepath.WalkDir(workspaceRoot, func(path string, directoryEntry os.DirEntry, err error) error {
		if err != nil || directoryEntry.IsDir() {
			if directoryEntry != nil && directoryEntry.IsDir() && strings.HasPrefix(directoryEntry.Name(), ".") && directoryEntry.Name() != "." {
				return filepath.SkipDir
			}
			return nil
		}
		name := filepath.Base(path)
		if name != "BUILD.star" && name != "BUILD.yaml" && name != "BUILD.yml" {
			return nil
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		packagePath, err := filepath.Rel(workspaceRoot, filepath.Dir(path))
		if err != nil || packagePath == "." {
			packagePath = ""
		}
		prefix := "//"
		if packagePath != "" {
			prefix += filepath.ToSlash(packagePath)
		}
		for _, declaration := range declarationsForFile(name, string(content)) {
			label := prefix + ":" + declaration.name
			labels = append(labels, label)
			if filepath.Clean(filepath.Dir(path)) == filepath.Clean(currentDirectory) {
				labels = append(labels, ":"+declaration.name)
			}
		}
		return nil
	})
	return labels
}

func declarationsForFile(fileName string, text string) []namedDeclaration {
	if fileName == "BUILD.star" {
		return starlarkNamedDeclarations(text)
	}
	var root yaml.Node
	if err := yaml.Unmarshal([]byte(text), &root); err != nil {
		return nil
	}
	if root.Kind == yaml.DocumentNode && len(root.Content) > 0 {
		root = *root.Content[0]
	}
	declarations := []namedDeclaration{}
	if root.Kind != yaml.MappingNode {
		return declarations
	}
	for index := 0; index+1 < len(root.Content); index += 2 {
		key := root.Content[index]
		value := root.Content[index+1]
		if key.Value != "targets" && key.Value != "aliases" && key.Value != "environments" || value.Kind != yaml.SequenceNode {
			continue
		}
		for _, item := range value.Content {
			nameNode := yamlMappingValue(item, "name")
			if nameNode != nil && nameNode.Value != "" {
				declarations = append(declarations, namedDeclaration{name: nameNode.Value})
			}
		}
	}
	return declarations
}

func starlarkLoadedSymbol(currentPath string, text string, identifier string) (string, string, bool) {
	loadPattern := regexp.MustCompile(`load\s*\(([^\n]*)\)`)
	matches := loadPattern.FindAllStringSubmatch(text, -1)
	for _, match := range matches {
		arguments := splitLoadArguments(match[1])
		if len(arguments) < 2 {
			continue
		}
		module := trimStarlarkString(arguments[0])
		if module == "" {
			continue
		}
		for _, argument := range arguments[1:] {
			localName := ""
			remoteName := ""
			parts := strings.SplitN(argument, "=", 2)
			if len(parts) == 2 {
				localName = strings.TrimSpace(parts[0])
				remoteName = trimStarlarkString(parts[1])
			} else {
				remoteName = trimStarlarkString(argument)
				localName = remoteName
			}
			if localName == identifier {
				return resolveStarlarkModulePath(currentPath, module), remoteName, true
			}
		}
	}
	return "", "", false
}

func splitLoadArguments(arguments string) []string {
	parts := []string{}
	start := 0
	inString := byte(0)
	for index := 0; index < len(arguments); index++ {
		character := arguments[index]
		if inString != 0 {
			if character == inString && (index == 0 || arguments[index-1] != '\\') {
				inString = 0
			}
			continue
		}
		if character == '\'' || character == '"' {
			inString = character
			continue
		}
		if character == ',' {
			parts = append(parts, strings.TrimSpace(arguments[start:index]))
			start = index + 1
		}
	}
	parts = append(parts, strings.TrimSpace(arguments[start:]))
	return parts
}

func trimStarlarkString(value string) string {
	value = strings.TrimSpace(value)
	if len(value) < 2 {
		return ""
	}
	if (value[0] == '\'' && value[len(value)-1] == '\'') || (value[0] == '"' && value[len(value)-1] == '"') {
		return value[1 : len(value)-1]
	}
	return ""
}

func resolveStarlarkModulePath(currentPath string, module string) string {
	if strings.HasPrefix(module, "//") {
		return filepath.Clean(strings.TrimPrefix(module, "//"))
	}
	if filepath.IsAbs(module) {
		return filepath.Clean(module)
	}
	return filepath.Clean(filepath.Join(filepath.Dir(currentPath), module))
}

func pathToURI(path string) string {
	return (&url.URL{Scheme: "file", Path: path}).String()
}

func starlarkIdentifierDefinitionRange(text string, identifier string) (rangeValue, bool) {
	patterns := []*regexp.Regexp{
		regexp.MustCompile(`^\s*def\s+` + regexp.QuoteMeta(identifier) + `\s*\(`),
		regexp.MustCompile(`^\s*` + regexp.QuoteMeta(identifier) + `\s*=`),
	}
	lines := strings.Split(text, "\n")
	for lineNumber, line := range lines {
		for _, pattern := range patterns {
			match := pattern.FindStringIndex(line)
			if match == nil {
				continue
			}
			start := strings.Index(line, identifier)
			if start < 0 {
				continue
			}
			return rangeValue{Start: position{Line: lineNumber, Character: start}, End: position{Line: lineNumber, Character: start + len(identifier)}}, true
		}
	}
	return rangeValue{}, false
}

func starlarkDeclarationRange(text string, targetName string) (rangeValue, bool) {
	patterns := []*regexp.Regexp{
		regexp.MustCompile(`\btarget\s*\([^\n]*\bname\s*=\s*["']` + regexp.QuoteMeta(targetName) + `["']`),
		regexp.MustCompile(`\balias\s*\([^\n]*\bname\s*=\s*["']` + regexp.QuoteMeta(targetName) + `["']`),
		regexp.MustCompile(`\benvironment\s*\([^\n]*\bname\s*=\s*["']` + regexp.QuoteMeta(targetName) + `["']`),
	}
	lines := strings.Split(text, "\n")
	for lineNumber, line := range lines {
		for _, pattern := range patterns {
			match := pattern.FindStringIndex(line)
			if match == nil {
				continue
			}
			nameIndex := strings.Index(line[match[0]:match[1]], targetName)
			if nameIndex < 0 {
				nameIndex = 0
			}
			start := match[0] + nameIndex
			return rangeValue{Start: position{Line: lineNumber, Character: start}, End: position{Line: lineNumber, Character: start + len(targetName)}}, true
		}
	}
	return rangeValue{}, false
}

func (s *server) documentSymbols(uri string) []map[string]any {
	return symbolsFromText(s.documents[uri])
}
func symbolsFromText(text string) []map[string]any { return nil }

func uriPath(uri string) string {
	parsed, err := url.Parse(uri)
	if err != nil || parsed.Scheme != "file" {
		return uri
	}
	return parsed.Path
}
