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
	field := yamlFieldAt(text, textPosition)
	if field == "dependencies" || field == "actual" {
		alreadyListed := map[string]bool{}
		if yamlBuildFieldIsCollection(field) {
			alreadyListed = yamlListValuesAt(text, textPosition, field)
		}
		return server.labelCompletionItems(path, text, textPosition, alreadyListed)
	} else if field == "inputs" || field == "exclude_inputs" || field == "bin_output" {
		alreadyListed := map[string]bool{}
		if yamlBuildFieldIsCollection(field) {
			alreadyListed = yamlListValuesAt(text, textPosition, field)
		}
		return pathCompletionItems(path, text, textPosition, alreadyListed)
	} else if field == "outputs" {
		return outputPathCompletionItems(path, text, textPosition, yamlListValuesAt(text, textPosition, field))
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
			alreadyListed := map[string]bool{}
			if loading.BuildFieldIsCollection("starlark", callName, field) {
				alreadyListed = stringListValuesAt(text, textPosition)
			}
			return server.labelCompletionItems(path, text, textPosition, alreadyListed)
		}
		if (field == "inputs" || field == "exclude_inputs" || field == "bin_output") && starlarkDeclarationHasField(callName, field) {
			alreadyListed := map[string]bool{}
			if loading.BuildFieldIsCollection("starlark", callName, field) {
				alreadyListed = stringListValuesAt(text, textPosition)
			}
			return pathCompletionItems(path, text, textPosition, alreadyListed)
		}
		if field == "outputs" && starlarkDeclarationHasField(callName, field) {
			return outputPathCompletionItems(path, text, textPosition, stringListValuesAt(text, textPosition))
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

func yamlBuildFieldIsCollection(field string) bool {
	for _, schema := range loading.BuildDeclarationSchemas("yaml") {
		if loading.BuildFieldIsCollection("yaml", schema.Kind, field) {
			return true
		}
	}
	return false
}

func (server *server) labelCompletionItems(currentPath string, text string, textPosition position, alreadyListed map[string]bool) []map[string]any {
	prefix := pathCompletionPrefix(text, textPosition)
	workspaceRoot := findWorkspaceRoot(filepath.Dir(currentPath))
	labels := server.collectWorkspaceLabels(workspaceRoot, filepath.Dir(currentPath))
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

func yamlListValuesAt(text string, textPosition position, field string) map[string]bool {
	lines := strings.Split(text, "\n")
	if textPosition.Line < 0 || textPosition.Line >= len(lines) {
		return map[string]bool{}
	}
	fieldLine := -1
	fieldIndent := 0
	for lineNumber := textPosition.Line; lineNumber >= 0; lineNumber-- {
		trimmedLine := strings.TrimSpace(lines[lineNumber])
		if name, _, found := strings.Cut(trimmedLine, ":"); found && strings.TrimSpace(name) == field {
			fieldLine = lineNumber
			fieldIndent = len(lines[lineNumber]) - len(strings.TrimLeft(lines[lineNumber], " \t"))
			break
		}
	}
	if fieldLine < 0 {
		return map[string]bool{}
	}
	if fieldLine == textPosition.Line && strings.Contains(lines[fieldLine], "[") {
		return stringListValuesAt(text, textPosition)
	}
	values := map[string]bool{}
	valuePattern := regexp.MustCompile(`^-\s*["']([^"']+)["']`)
	for lineNumber := fieldLine + 1; lineNumber < textPosition.Line; lineNumber++ {
		line := lines[lineNumber]
		trimmedLine := strings.TrimSpace(line)
		indent := len(line) - len(strings.TrimLeft(line, " \t"))
		if trimmedLine != "" && indent <= fieldIndent {
			break
		}
		if matches := valuePattern.FindStringSubmatch(trimmedLine); len(matches) == 2 {
			values[matches[1]] = true
		}
	}
	return values
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

func outputPathCompletionItems(currentPath string, text string, textPosition position, alreadyListed map[string]bool) []map[string]any {
	prefix := pathCompletionPrefix(text, textPosition)
	outputTypes := []struct {
		prefix          string
		directoriesOnly bool
	}{{prefix: "dir::", directoriesOnly: true}, {prefix: "file::"}}
	for _, outputType := range outputTypes {
		if strings.HasPrefix(prefix, outputType.prefix) {
			items := pathCompletionItemsWithPrefix(currentPath, strings.TrimPrefix(prefix, outputType.prefix), outputType.prefix, outputType.directoriesOnly, alreadyListed)
			addCompletionTextEdits(items, textPosition, prefix)
			return items
		}
	}
	items := pathCompletionItemsWithPrefix(currentPath, prefix, "", false, alreadyListed)
	addCompletionTextEdits(items, textPosition, prefix)
	return items
}

func pathCompletionItems(currentPath string, text string, textPosition position, alreadyListed map[string]bool) []map[string]any {
	prefix := pathCompletionPrefix(text, textPosition)
	items := pathCompletionItemsWithPrefix(currentPath, prefix, "", false, alreadyListed)
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
	stringDelimiter := byte(0)
	quoteStart := -1
	for index := 0; index < len(prefix); index++ {
		character := prefix[index]
		if stringDelimiter != 0 {
			if character == stringDelimiter && !starlarkCharacterEscaped(prefix, index) {
				stringDelimiter = 0
				quoteStart = -1
			}
			continue
		}
		if character == '\'' || character == '"' {
			stringDelimiter = character
			quoteStart = index
		}
	}
	if quoteStart >= 0 {
		return prefix[quoteStart+1:]
	}
	start := strings.LastIndexAny(prefix, " \t,[](){}=") + 1
	return prefix[start:]
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
	equals := -1
	stringDelimiter := ""
	inComment := false
	for index := 0; index < len(prefix); index++ {
		character := prefix[index]
		if inComment {
			if character == '\n' {
				inComment = false
			}
			continue
		}
		if stringDelimiter != "" {
			if strings.HasPrefix(prefix[index:], stringDelimiter) && !starlarkCharacterEscaped(prefix, index) {
				index += len(stringDelimiter) - 1
				stringDelimiter = ""
			}
			continue
		}
		if character == '\'' || character == '"' {
			stringDelimiter = starlarkStringDelimiter(prefix, index)
			index += len(stringDelimiter) - 1
		} else if character == '#' {
			inComment = true
		} else if character == '=' {
			equals = index
		}
	}
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
	stringDelimiter := ""
	inComment := false
	for index := 0; index < offset; index++ {
		character := text[index]
		if inComment {
			if character == '\n' {
				inComment = false
			}
			continue
		}
		if stringDelimiter != "" {
			if strings.HasPrefix(text[index:], stringDelimiter) && !starlarkCharacterEscaped(text, index) {
				index += len(stringDelimiter) - 1
				stringDelimiter = ""
			}
			continue
		}
		if character == '\'' || character == '"' {
			stringDelimiter = starlarkStringDelimiter(text, index)
			index += len(stringDelimiter) - 1
		} else if character == '#' {
			inComment = true
		}
	}
	return stringDelimiter != ""
}

func starlarkStringDelimiter(text string, index int) string {
	delimiter := string(text[index])
	if strings.HasPrefix(text[index:], delimiter+delimiter+delimiter) {
		return delimiter + delimiter + delimiter
	}
	return delimiter
}

func starlarkCharacterEscaped(text string, index int) bool {
	backslashes := 0
	for index > backslashes && text[index-backslashes-1] == '\\' {
		backslashes++
	}
	return backslashes%2 == 1
}

func yamlFieldAt(text string, textPosition position) string {
	lines := strings.Split(text, "\n")
	if textPosition.Line < 0 || textPosition.Line >= len(lines) {
		return ""
	}
	currentLine := lines[textPosition.Line]
	currentIndent := len(currentLine) - len(strings.TrimLeft(currentLine, " \t"))
	characterOffset := characterByteOffset(currentLine, textPosition.Character)
	if characterOffset < 0 {
		return ""
	}
	activeField := ""
	stringDelimiter := byte(0)
	for index := 0; index < characterOffset; index++ {
		character := currentLine[index]
		if stringDelimiter != 0 {
			if character == stringDelimiter && !starlarkCharacterEscaped(currentLine, index) {
				stringDelimiter = 0
			}
			continue
		}
		if character == '\'' || character == '"' {
			stringDelimiter = character
			continue
		}
		if character != ':' {
			continue
		}
		if index+1 < len(currentLine) && !unicodeSpace(currentLine[index+1]) && !strings.ContainsRune("[{\"'},]", rune(currentLine[index+1])) {
			continue
		}
		start := index
		for start > 0 && isWordCharacter(currentLine[start-1]) {
			start--
		}
		if start > 0 && !unicodeSpace(currentLine[start-1]) && !strings.ContainsRune("[{,-", rune(currentLine[start-1])) {
			continue
		}
		if yamlFieldName(currentLine[start:index]) {
			activeField = currentLine[start:index]
		}
	}
	if activeField != "" {
		return activeField
	}
	for lineNumber := textPosition.Line; lineNumber >= 0 && lineNumber < len(lines); lineNumber-- {
		rawLine := lines[lineNumber]
		indent := len(rawLine) - len(strings.TrimLeft(rawLine, " \t"))
		line := strings.TrimSpace(rawLine)
		line = strings.TrimSpace(strings.TrimPrefix(line, "-"))
		if field, _, found := strings.Cut(line, ":"); found && yamlFieldName(field) {
			if lineNumber == textPosition.Line || indent < currentIndent {
				return strings.TrimSpace(field)
			}
		}
		if lineNumber < textPosition.Line && strings.HasPrefix(strings.TrimSpace(rawLine), "-") && indent <= currentIndent {
			return ""
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
	name, _ := enclosingStarlarkCallAt(text, textPosition)
	return name
}

func enclosingStarlarkCallAt(text string, textPosition position) (string, int) {
	offset := byteOffset(text, textPosition)
	if offset < 0 || offset > len(text) {
		return "", -1
	}
	type call struct {
		name          string
		openingOffset int
	}
	calls := []call{}
	stringDelimiter := ""
	inComment := false
	for index := 0; index < offset; index++ {
		character := text[index]
		if inComment {
			if character == '\n' {
				inComment = false
			}
			continue
		}
		if stringDelimiter != "" {
			if strings.HasPrefix(text[index:], stringDelimiter) && !starlarkCharacterEscaped(text, index) {
				index += len(stringDelimiter) - 1
				stringDelimiter = ""
			}
			continue
		}
		if character == '\'' || character == '"' {
			stringDelimiter = starlarkStringDelimiter(text, index)
			index += len(stringDelimiter) - 1
			continue
		}
		if character == '#' {
			inComment = true
			continue
		}
		switch character {
		case '(':
			end := index
			for end > 0 && unicodeSpace(text[end-1]) {
				end--
			}
			start := end
			for start > 0 && isWordCharacter(text[start-1]) {
				start--
			}
			calls = append(calls, call{name: text[start:end], openingOffset: index})
		case ')':
			if len(calls) > 0 {
				calls = calls[:len(calls)-1]
			}
		}
	}
	for index := len(calls) - 1; index >= 0; index-- {
		if slices.Contains(loading.StarlarkBuiltinNames(), calls[index].name) {
			return calls[index].name, calls[index].openingOffset
		}
	}
	return "", -1
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
	declarationKind, openingOffset := enclosingStarlarkCallAt(text, textPosition)
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
	return map[string]any{"signatures": []map[string]any{{"label": label}}, "activeSignature": 0, "activeParameter": starlarkActiveParameter(text, textPosition, openingOffset, parameters)}
}

func starlarkActiveParameter(text string, textPosition position, openingOffset int, parameters []loading.StarlarkParameter) int {
	offset := byteOffset(text, textPosition)
	if offset < 0 || offset > len(text) || openingOffset < 0 || openingOffset >= offset {
		return 0
	}
	arguments := text[openingOffset+1 : offset]
	activeParameter := 0
	segmentStart := 0
	equalsOffset := -1
	depth := 0
	stringDelimiter := ""
	inComment := false
	for index := 0; index < len(arguments); index++ {
		character := arguments[index]
		if inComment {
			if character == '\n' {
				inComment = false
			}
			continue
		}
		if stringDelimiter != "" {
			if strings.HasPrefix(arguments[index:], stringDelimiter) && !starlarkCharacterEscaped(arguments, index) {
				index += len(stringDelimiter) - 1
				stringDelimiter = ""
			}
			continue
		}
		switch character {
		case '\'', '"':
			stringDelimiter = starlarkStringDelimiter(arguments, index)
			index += len(stringDelimiter) - 1
		case '#':
			inComment = true
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			if depth > 0 {
				depth--
			}
		case ',':
			if depth == 0 {
				activeParameter++
				segmentStart = index + 1
				equalsOffset = -1
			}
		case '=':
			if depth == 0 {
				equalsOffset = index
			}
		}
	}
	if equalsOffset >= segmentStart {
		name := strings.TrimSpace(arguments[segmentStart:equalsOffset])
		for index, parameter := range parameters {
			if parameter.Name == name {
				return index
			}
		}
	}
	return min(activeParameter, len(parameters)-1)
}
