package lsp

import (
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"go.starlark.net/syntax"
	"gopkg.in/yaml.v3"
)

func (server *server) definition(documentURI string, textPosition position) any {
	path := uriPath(documentURI)
	if !isStarlarkFile(filepath.Base(path)) {
		return nil
	}
	text := server.documentText(documentURI)
	label := labelAt(text, textPosition)
	if label != "" {
		return server.labelDefinition(path, label)
	}
	word := wordAt(text, textPosition)
	if word == "" {
		return nil
	}
	definitionRange, found := starlarkIdentifierDefinitionRange(text, word)
	if found {
		return map[string]any{"uri": documentURI, "range": definitionRange}
	}
	modulePath, moduleSymbol, found := starlarkLoadedSymbol(path, text, word)
	if !found {
		return nil
	}
	moduleURI := pathToURI(modulePath)
	moduleText := server.documentText(moduleURI)
	if moduleText == "" {
		return nil
	}
	definitionRange, found = starlarkIdentifierDefinitionRange(moduleText, moduleSymbol)
	if !found {
		return nil
	}
	return map[string]any{"uri": moduleURI, "range": definitionRange}
}

func (server *server) labelDefinition(currentPath string, label string) any {
	targetName := strings.TrimPrefix(label, ":")
	directory := filepath.Dir(currentPath)
	if strings.HasPrefix(label, "//") {
		colon := strings.LastIndex(label, ":")
		if colon < 0 {
			return nil
		}
		directory = filepath.Join(findWorkspaceRoot(filepath.Dir(currentPath)), strings.TrimPrefix(label[:colon], "//"))
		targetName = label[colon+1:]
	}
	for _, fileName := range []string{"BUILD.star", "BUILD.bzl", "BUILD.yaml", "BUILD.yml"} {
		definitionPath := filepath.Join(directory, fileName)
		definitionURI := pathToURI(definitionPath)
		definitionText := server.documentText(definitionURI)
		if definitionText == "" {
			continue
		}
		for _, declaration := range declarationsForFile(fileName, definitionText) {
			if declaration.name == targetName {
				return map[string]any{"uri": definitionURI, "range": declaration.rangeValue}
			}
		}
	}
	return nil
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
		if _, operationError := os.Stat(filepath.Join(directory, "grog.toml")); operationError == nil {
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
	_ = filepath.WalkDir(workspaceRoot, func(path string, directoryEntry os.DirEntry, operationError error) error {
		if operationError != nil || directoryEntry.IsDir() {
			if directoryEntry != nil && directoryEntry.IsDir() && strings.HasPrefix(directoryEntry.Name(), ".") && directoryEntry.Name() != "." {
				return filepath.SkipDir
			}
			return nil
		}
		name := filepath.Base(path)
		if name != "BUILD.star" && name != "BUILD.bzl" && name != "BUILD.yaml" && name != "BUILD.yml" {
			return nil
		}
		content, operationError := os.ReadFile(path)
		if operationError != nil {
			return nil
		}
		packagePath, operationError := filepath.Rel(workspaceRoot, filepath.Dir(path))
		if operationError != nil || packagePath == "." {
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
	if isStarlarkFile(fileName) {
		return starlarkNamedDeclarations(text)
	}
	var root yaml.Node
	if operationError := yaml.Unmarshal([]byte(text), &root); operationError != nil {
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
		kind, isDeclarationList := map[string]string{"targets": "target", "aliases": "alias", "resources": "resource", "environments": "environment"}[key.Value]
		if !isDeclarationList || value.Kind != yaml.SequenceNode {
			continue
		}
		for _, item := range value.Content {
			nameNode := yamlMappingValue(item, "name")
			if nameNode != nil && nameNode.Value != "" {
				declarations = append(declarations, namedDeclaration{kind: kind, name: nameNode.Value, rangeValue: yamlNodeRange(text, nameNode)})
			}
		}
	}
	return declarations
}

func starlarkLoadedSymbol(currentPath string, text string, identifier string) (string, string, bool) {
	file, operationError := syntax.Parse(currentPath, text, 0)
	if operationError != nil {
		return "", "", false
	}
	for _, statement := range file.Stmts {
		loadStatement, isLoad := statement.(*syntax.LoadStmt)
		if !isLoad {
			continue
		}
		for index, localIdentifier := range loadStatement.To {
			if localIdentifier.Name == identifier {
				return resolveStarlarkModulePath(currentPath, loadStatement.ModuleName()), loadStatement.From[index].Name, true
			}
		}
	}
	return "", "", false
}

func resolveStarlarkModulePath(currentPath string, module string) string {
	if strings.HasPrefix(module, "//") {
		return filepath.Join(findWorkspaceRoot(filepath.Dir(currentPath)), strings.TrimPrefix(module, "//"))
	}
	if filepath.IsAbs(module) {
		return filepath.Clean(module)
	}
	return filepath.Clean(filepath.Join(filepath.Dir(currentPath), module))
}

func isStarlarkFile(fileName string) bool {
	return fileName == "BUILD.star" || fileName == "BUILD.bzl" || strings.HasSuffix(fileName, ".star") || strings.HasSuffix(fileName, ".bzl")
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

func (server *server) documentSymbols(documentURI string) []map[string]any {
	return symbolsFromText(filepath.Base(uriPath(documentURI)), server.documentText(documentURI))
}

func symbolsFromText(fileName string, text string) []map[string]any {
	declarations := declarationsForFile(fileName, text)
	symbols := make([]map[string]any, 0, len(declarations))
	for _, declaration := range declarations {
		symbols = append(symbols, map[string]any{
			"name":           declaration.name,
			"detail":         declaration.kind,
			"kind":           19,
			"range":          declaration.rangeValue,
			"selectionRange": declaration.rangeValue,
		})
	}
	if !isStarlarkFile(fileName) {
		return symbols
	}
	file, operationError := syntax.Parse(fileName, text, 0)
	if operationError != nil {
		return symbols
	}
	for _, statement := range file.Stmts {
		definition, isDefinition := statement.(*syntax.DefStmt)
		if !isDefinition {
			continue
		}
		definitionStart, definitionEnd := definition.Span()
		nameStart, nameEnd := definition.Name.Span()
		definitionRange := rangeFromSyntaxPositions(text, definitionStart, definitionEnd)
		selectionRange := rangeFromSyntaxPositions(text, nameStart, nameEnd)
		symbols = append(symbols, map[string]any{
			"name":           definition.Name.Name,
			"detail":         "macro",
			"kind":           12,
			"range":          definitionRange,
			"selectionRange": selectionRange,
		})
	}
	return symbols
}

func rangeFromSyntaxPositions(text string, start syntax.Position, end syntax.Position) rangeValue {
	return rangeValue{Start: positionFromSyntaxPosition(text, start), End: positionFromSyntaxPosition(text, end)}
}

func positionFromSyntaxPosition(text string, syntaxPosition syntax.Position) position {
	return positionFromOneBased(text, int(syntaxPosition.Line), int(syntaxPosition.Col))
}

func positionFromOneBased(text string, line int, column int) position {
	lineNumber := max(line-1, 0)
	character := max(column-1, 0)
	lines := strings.Split(text, "\n")
	if lineNumber >= len(lines) {
		return position{Line: lineNumber}
	}
	runes := []rune(lines[lineNumber])
	if character > len(runes) {
		character = len(runes)
	}
	return position{Line: lineNumber, Character: len(string(runes[:character]))}
}

func uriPath(documentURI string) string {
	parsed, operationError := url.Parse(documentURI)
	if operationError != nil || parsed.Scheme != "file" {
		return documentURI
	}
	return parsed.Path
}
