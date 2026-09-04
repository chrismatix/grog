package lsp

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"grog/internal/loading"
)

func (server *server) completionItems(documentURI string, textPosition position) []map[string]any {
	path := uriPath(documentURI)
	text := server.documentText(documentURI)
	name := filepath.Base(path)
	if isStarlarkFile(name) {
		return server.starlarkCompletionItems(documentURI, text, textPosition)
	}
	if field := yamlFieldAt(text, textPosition); field == "dependencies" || field == "actual" {
		return server.labelCompletionItems(path, text, textPosition)
	} else if field == "inputs" || field == "exclude_inputs" || field == "bin_output" {
		return pathCompletionItems(path, text, textPosition)
	} else if field == "outputs" {
		return outputPathCompletionItems(path, text, textPosition)
	}
	return completionItemsFor(allBuildFieldNames("yaml"), 5)
}

func (server *server) documentText(documentURI string) string {
	if text, isOpen := server.documents[documentURI]; isOpen {
		return text
	}
	text, operationError := os.ReadFile(uriPath(documentURI))
	if operationError != nil {
		return ""
	}
	return string(text)
}

func (server *server) starlarkCompletionItems(documentURI string, text string, textPosition position) []map[string]any {
	path := uriPath(documentURI)
	field := starlarkFieldAt(text, textPosition)
	callName := enclosingStarlarkCall(text, textPosition)
	if inStringAt(text, textPosition) {
		if (field == "dependencies" || field == "actual") && starlarkDeclarationHasField(callName, field) {
			return server.labelCompletionItems(path, text, textPosition)
		}
		if (field == "inputs" || field == "exclude_inputs" || field == "bin_output") && starlarkDeclarationHasField(callName, field) {
			return pathCompletionItems(path, text, textPosition)
		}
		if field == "outputs" && starlarkDeclarationHasField(callName, field) {
			return outputPathCompletionItems(path, text, textPosition)
		}
		return nil
	}
	if callName != "" && !shouldSuggestStarlarkCallParameters(text, textPosition) {
		return nil
	}
	if parameters := loading.StarlarkParameters(callName); callName != "" && len(parameters) > 0 {
		names := make([]string, 0, len(parameters))
		for _, parameter := range parameters {
			names = append(names, parameter.Name)
		}
		return completionItemsFor(names, 5)
	}
	builtins := append(loading.StarlarkBuiltinNames(), "load")
	items := completionItemsFor(builtins, 3)
	items = append(items, completionItemsFor(loading.StarlarkGlobalNames(), 6)...)
	return items
}

func allBuildFieldNames(format string) []string {
	names := loading.BuildFieldNames(format, "")
	seen := map[string]bool{}
	for _, name := range names {
		seen[name] = true
	}
	for _, schema := range loading.BuildDeclarationSchemas(format) {
		for _, name := range loading.BuildFieldNames(format, schema.Kind) {
			if !seen[name] {
				names = append(names, name)
				seen[name] = true
			}
		}
	}
	return names
}

func starlarkDeclarationHasField(declarationKind string, field string) bool {
	for _, parameter := range loading.StarlarkParameters(declarationKind) {
		if parameter.Name == field {
			return true
		}
	}
	return false
}

func (server *server) labelCompletionItems(currentPath string, text string, textPosition position) []map[string]any {
	prefix := pathCompletionPrefix(text, textPosition)
	workspaceRoot := findWorkspaceRoot(filepath.Dir(currentPath))
	labels := server.collectWorkspaceLabels(workspaceRoot, filepath.Dir(currentPath))
	alreadyListed := stringListValuesAt(text, textPosition)
	items := []map[string]any{}
	for _, label := range preferredDependencyLabels(labels, prefix) {
		if prefix != "" && !strings.HasPrefix(label, prefix) || alreadyListed[label] {
			continue
		}
		items = append(items, map[string]any{"label": label, "kind": 12, "insertText": label, "sortText": fmt.Sprintf("%03d_%s", len(items), label), "documentation": "grog target label", "textEdit": completionTextEdit(textPosition, prefix, label)})
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
			items := pathCompletionItemsWithPrefix(currentPath, strings.TrimPrefix(prefix, outputTypePrefix), outputTypePrefix, true, stringListValuesAt(text, textPosition))
			addCompletionTextEdits(items, textPosition, prefix)
			return items
		}
	}
	items := pathCompletionItemsWithPrefix(currentPath, prefix, "", false, stringListValuesAt(text, textPosition))
	addCompletionTextEdits(items, textPosition, prefix)
	return items
}

func pathCompletionItems(currentPath string, text string, textPosition position) []map[string]any {
	prefix := pathCompletionPrefix(text, textPosition)
	items := pathCompletionItemsWithPrefix(currentPath, prefix, "", false, stringListValuesAt(text, textPosition))
	addCompletionTextEdits(items, textPosition, prefix)
	return items
}

func addCompletionTextEdits(items []map[string]any, textPosition position, prefix string) {
	for _, item := range items {
		label, isString := item["label"].(string)
		if isString {
			item["textEdit"] = completionTextEdit(textPosition, prefix, label)
		}
	}
}

func completionTextEdit(textPosition position, prefix string, newText string) map[string]any {
	start := position{Line: textPosition.Line, Character: max(textPosition.Character-utf16Length(prefix), 0)}
	return map[string]any{"range": rangeValue{Start: start, End: textPosition}, "newText": newText}
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
	entries, operationError := os.ReadDir(entryDirectory)
	if operationError != nil {
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
	characterOffset := characterByteOffset(line, textPosition.Character)
	if characterOffset < 0 {
		return ""
	}
	prefix := line[:characterOffset]
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
	"resource":              "Declare a long-running resource used by targets.",
	"environment":           "Declare an execution environment.",
	"load":                  "Load symbols from another Starlark file using grog's local load resolution. No Bazel repositories are fetched.",
	"name":                  "The declaration name.",
	"command":               "Shell command run by a target.",
	"dependencies":          "Target labels this item depends on, such as :build or //pkg:test.",
	"inputs":                "Input files or globs used to fingerprint a target.",
	"exclude_inputs":        "Input globs to exclude from fingerprinting.",
	"outputs":               "Output paths produced by a target.",
	"bin_output":            "Executable output path produced by a target.",
	"binary_requires_push":  "Require --push before running this target's binary output.",
	"output_checks":         "Checks that validate produced outputs.",
	"tags":                  "Tags used for filtering targets.",
	"fingerprint":           "Additional key/value fingerprint material.",
	"platforms":             "Platform selectors that constrain where this declaration applies.",
	"environment_variables": "Environment variables for the target.",
	"timeout":               "Target timeout duration.",
	"concurrency_group":     "Concurrency group used to serialize related targets.",
	"oci_push":              "OCI push destinations for declared OCI outputs.",
	"actual":                "Target label referenced by an alias.",
	"up":                    "Command that starts a resource.",
	"down":                  "Command that stops a resource.",
	"ready":                 "Command that reports when a resource is ready.",
	"exports":               "Environment variables exported by a resource.",
	"type":                  "Environment type.",
	"oci_image":             "OCI image used by an environment.",
	"targets":               "YAML list of grog targets.",
	"aliases":               "YAML list of grog aliases.",
	"resources":             "YAML list of grog resources.",
	"environments":          "YAML list of grog environments.",
	"default_platforms":     "Default platform selectors for this package.",
	"GROG_OS":               "Target operating system selected by grog.",
	"GROG_ARCH":             "Target architecture selected by grog.",
	"GROG_PLATFORM":         "Selected operating system and architecture.",
	"GROG_PLATFORM_TAGS":    "Active custom platform tags.",
	"GROG_ENV_FILE":         "Resolved environment variables file path.",
	"GROG_WORKSPACE_ROOT":   "Absolute path to the grog workspace.",
	"GROG_GIT_HASH":         "Current Git commit hash.",
	"json":                  "Starlark JSON module.",
	"math":                  "Starlark math module.",
	"time":                  "Starlark time module.",
}

func (server *server) hover(documentURI string, textPosition position) any {
	word := wordAt(server.documentText(documentURI), textPosition)
	if word == "" {
		return nil
	}
	documentation, isDocumented := docs[word]
	if !isDocumented {
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
	inComment := false
	for index := 0; index < offset; index++ {
		character := text[index]
		if inComment {
			if character == '\n' {
				inComment = false
			}
			continue
		}
		if inString != 0 {
			if character == inString && (index == 0 || text[index-1] != '\\') {
				inString = 0
			}
			continue
		}
		if character == '\'' || character == '"' {
			inString = character
		} else if character == '#' {
			inComment = true
		}
	}
	return inString != 0
}

func yamlFieldAt(text string, textPosition position) string {
	lines := strings.Split(text, "\n")
	for lineNumber := textPosition.Line; lineNumber >= 0 && lineNumber < len(lines); lineNumber-- {
		line := strings.TrimSpace(lines[lineNumber])
		line = strings.TrimSpace(strings.TrimPrefix(line, "-"))
		if field, _, found := strings.Cut(line, ":"); found && yamlFieldName(field) {
			return strings.TrimSpace(field)
		}
	}
	return ""
}

func yamlFieldName(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	for index := range len(value) {
		if !isWordCharacter(value[index]) {
			return false
		}
	}
	return true
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
	callNames := []string{}
	inString := byte(0)
	inComment := false
	for index := 0; index < offset; index++ {
		character := text[index]
		if inComment {
			if character == '\n' {
				inComment = false
			}
			continue
		}
		if inString != 0 {
			if character == inString && (index == 0 || text[index-1] != '\\') {
				inString = 0
			}
			continue
		}
		if character == '\'' || character == '"' {
			inString = character
			continue
		}
		if character == '#' {
			inComment = true
			continue
		}
		switch character {
		case '(':
			end := index
			start := end
			for start > 0 && isWordCharacter(text[start-1]) {
				start--
			}
			callNames = append(callNames, text[start:end])
		case ')':
			if len(callNames) > 0 {
				callNames = callNames[:len(callNames)-1]
			}
		}
	}
	for index := len(callNames) - 1; index >= 0; index-- {
		name := callNames[index]
		if slices.Contains(loading.StarlarkBuiltinNames(), name) {
			return name
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
	for offset, currentRune := range text {
		if offset >= targetOffset {
			break
		}
		if currentRune == '\n' {
			line++
			character = 0
			continue
		}
		character += utf16RuneLength(currentRune)
	}
	return position{Line: line, Character: character}
}

func byteOffset(text string, textPosition position) int {
	if textPosition.Line < 0 || textPosition.Character < 0 {
		return -1
	}
	line := 0
	character := 0
	for offset, currentRune := range text {
		if line == textPosition.Line && character == textPosition.Character {
			return offset
		}
		if currentRune == '\n' {
			line++
			character = 0
			continue
		}
		character += utf16RuneLength(currentRune)
	}
	if line == textPosition.Line && character == textPosition.Character {
		return len(text)
	}
	return -1
}

func characterByteOffset(line string, character int) int {
	return byteOffset(line, position{Character: character})
}

func utf16Length(value string) int {
	length := 0
	for _, currentRune := range value {
		length += utf16RuneLength(currentRune)
	}
	return length
}

func utf16RuneLength(currentRune rune) int {
	if currentRune > 0xffff {
		return 2
	}
	return 1
}

func wordAt(text string, textPosition position) string {
	lines := strings.Split(text, "\n")
	if textPosition.Line < 0 || textPosition.Line >= len(lines) {
		return ""
	}
	line := lines[textPosition.Line]
	characterOffset := characterByteOffset(line, textPosition.Character)
	if characterOffset < 0 {
		return ""
	}
	start := characterOffset
	for start > 0 && isWordCharacter(line[start-1]) {
		start--
	}
	end := characterOffset
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

func signatureHelp(text string, textPosition position) any {
	declarationKind := enclosingStarlarkCall(text, textPosition)
	parameters := loading.StarlarkParameters(declarationKind)
	if len(parameters) == 0 {
		return nil
	}
	names := make([]string, 0, len(parameters))
	for _, parameter := range parameters {
		name := parameter.Name
		if !parameter.Required {
			name += "?"
		}
		names = append(names, name)
	}
	label := declarationKind + "(" + strings.Join(names, ", ") + ")"
	return map[string]any{"signatures": []map[string]any{{"label": label}}, "activeSignature": 0, "activeParameter": 0}
}
