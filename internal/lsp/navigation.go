package lsp

import (
	"context"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"

	"grog/internal/config"
	"grog/internal/label"
	"grog/internal/loading"

	"go.starlark.net/syntax"
	"gopkg.in/yaml.v3"
)

type indexedLabel struct {
	path string
	name string
}

func (server *server) definition(documentURI string, textPosition position) any {
	path := uriPath(documentURI)
	text := server.documentText(documentURI)
	targetLabel := labelAt(text, textPosition)
	if targetLabel != "" {
		return server.labelDefinition(path, targetLabel)
	}
	if !isStarlarkFile(filepath.Base(path)) {
		return nil
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

func (server *server) labelDefinition(currentPath string, targetLabel string) any {
	workspaceRoot := findWorkspaceRoot(filepath.Dir(currentPath))
	packagePath, operationError := filepath.Rel(workspaceRoot, filepath.Dir(currentPath))
	if operationError != nil {
		return nil
	}
	parsedLabel, operationError := label.ParseTargetLabel(filepath.ToSlash(packagePath), targetLabel)
	if operationError != nil {
		return nil
	}
	directory := filepath.Join(workspaceRoot, filepath.FromSlash(parsedLabel.Package))
	fileNames := []string{}
	if filepath.Clean(directory) == filepath.Clean(filepath.Dir(currentPath)) {
		fileNames = append(fileNames, filepath.Base(currentPath))
	}
	entries, operationError := os.ReadDir(directory)
	if operationError == nil {
		for _, entry := range entries {
			fileName := entry.Name()
			if !entry.IsDir() && isSupportedBuildFile(fileName) && !slices.Contains(fileNames, fileName) {
				fileNames = append(fileNames, fileName)
			}
		}
	}
	for _, fileName := range fileNames {
		definitionPath := filepath.Join(directory, fileName)
		definitionURI := pathToURI(definitionPath)
		definitionText := server.documentText(definitionURI)
		if definitionText == "" {
			continue
		}
		for _, declaration := range server.declarationsForPath(definitionPath, definitionText) {
			if declaration.name == parsedLabel.Name && loading.IsBuildLabelKind(declaration.kind) {
				if declaration.path != "" {
					definitionURI = pathToURI(declaration.path)
				}
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
	characterOffset := characterByteOffset(line, textPosition.Character)
	if characterOffset < 0 {
		return ""
	}
	if quoteStart := strings.LastIndexAny(line[:characterOffset], "\"'"); quoteStart >= 0 {
		quote := line[quoteStart]
		if quoteEnd := strings.IndexByte(line[characterOffset:], quote); quoteEnd >= 0 {
			candidate := line[quoteStart+1 : characterOffset+quoteEnd]
			if _, operationError := label.ParseTargetLabel("", candidate); operationError == nil {
				return candidate
			}
		}
	}
	start := strings.LastIndexAny(line[:characterOffset], " \t,[](){}=") + 1
	endRelative := strings.IndexAny(line[characterOffset:], " \t,[](){}=")
	end := len(line)
	if endRelative >= 0 {
		end = characterOffset + endRelative
	}
	candidate := strings.Trim(line[start:end], "\"'")
	if _, operationError := label.ParseTargetLabel("", candidate); operationError == nil {
		return candidate
	}
	return ""
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

func (server *server) collectWorkspaceLabels(workspaceRoot string, currentDirectory string) []string {
	indexedLabels, isCached := server.workspaceLabels[workspaceRoot]
	if !isCached {
		indexedLabels = server.indexWorkspaceLabels(workspaceRoot)
		if server.workspaceLabels == nil {
			server.workspaceLabels = make(map[string][]indexedLabel)
		}
		server.workspaceLabels[workspaceRoot] = indexedLabels
	}
	openBuildFiles := make(map[string]bool)
	openLabels := []indexedLabel{}
	for documentURI, text := range server.documents {
		path := uriPath(documentURI)
		if !isSupportedBuildFile(filepath.Base(path)) || !pathWithinWorkspace(workspaceRoot, path) {
			continue
		}
		openBuildFiles[filepath.Clean(path)] = true
		for _, declaration := range server.declarationsForPath(path, text) {
			if loading.IsBuildLabelKind(declaration.kind) {
				openLabels = append(openLabels, indexedLabel{path: path, name: declaration.name})
			}
		}
	}
	labelsToRender := make([]indexedLabel, 0, len(indexedLabels)+len(openLabels))
	for _, indexedLabel := range indexedLabels {
		if !openBuildFiles[filepath.Clean(indexedLabel.path)] {
			labelsToRender = append(labelsToRender, indexedLabel)
		}
	}
	labelsToRender = append(labelsToRender, openLabels...)
	labels := []string{}
	seen := make(map[string]bool)
	for _, indexedLabel := range labelsToRender {
		packagePath, operationError := filepath.Rel(workspaceRoot, filepath.Dir(indexedLabel.path))
		if operationError != nil || packagePath == "." {
			packagePath = ""
		}
		label := "//" + filepath.ToSlash(packagePath) + ":" + indexedLabel.name
		if !seen[label] {
			labels = append(labels, label)
			seen[label] = true
		}
		localLabel := ":" + indexedLabel.name
		if filepath.Clean(filepath.Dir(indexedLabel.path)) == filepath.Clean(currentDirectory) && !seen[localLabel] {
			labels = append(labels, localLabel)
			seen[localLabel] = true
		}
	}
	return labels
}

func (server *server) indexWorkspaceLabels(workspaceRoot string) []indexedLabel {
	labels := []indexedLabel{}
	includeHidden := loading.WorkspaceIncludesHidden(workspaceRoot)
	evaluationOptions := loading.StarlarkOptionsForWorkspace(workspaceRoot)
	_ = filepath.WalkDir(workspaceRoot, func(path string, directoryEntry os.DirEntry, operationError error) error {
		if operationError != nil || directoryEntry.IsDir() {
			if !includeHidden && directoryEntry != nil && directoryEntry.IsDir() && filepath.Clean(path) != filepath.Clean(workspaceRoot) && strings.HasPrefix(directoryEntry.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if !isSupportedBuildFile(filepath.Base(path)) {
			return nil
		}
		for _, declaration := range server.declarationsForPathWithOptions(path, server.documentText(pathToURI(path)), evaluationOptions) {
			if loading.IsBuildLabelKind(declaration.kind) {
				labels = append(labels, indexedLabel{path: path, name: declaration.name})
			}
		}
		return nil
	})
	return labels
}

func (server *server) invalidateLabelIndexForDocument(documentURI string) {
	if loading.IsStarlarkSourceFile(filepath.Base(uriPath(documentURI))) {
		server.invalidateLabelIndex()
	}
}

func (server *server) invalidateLabelIndex() {
	clear(server.workspaceLabels)
}

func pathWithinWorkspace(workspaceRoot string, path string) bool {
	relativePath, operationError := filepath.Rel(workspaceRoot, path)
	return operationError == nil && relativePath != ".." && !strings.HasPrefix(relativePath, ".."+string(filepath.Separator))
}

func (server *server) declarationsForPath(path string, text string) []namedDeclaration {
	if !isStarlarkFile(filepath.Base(path)) {
		return packageFileDeclarations(path, text)
	}
	options := loading.StarlarkOptionsForWorkspace(findWorkspaceRoot(filepath.Dir(path)))
	return server.declarationsForPathWithOptions(path, text, options)
}

func (server *server) declarationsForPathWithOptions(path string, text string, options loading.StarlarkEvaluationOptions) []namedDeclaration {
	if !isStarlarkFile(filepath.Base(path)) {
		return packageFileDeclarations(path, text)
	}
	declarations, operationError := evaluateStarlarkWithOptions(path, text, func(modulePath string) (string, error) {
		moduleURI := pathToURI(modulePath)
		if moduleText, isOpen := server.documents[moduleURI]; isOpen {
			return moduleText, nil
		}
		content, readError := os.ReadFile(modulePath)
		return string(content), readError
	}, options)
	if operationError != nil {
		return starlarkNamedDeclarations(text)
	}
	return declarations
}

func packageFileDeclarations(path string, text string) []namedDeclaration {
	if loading.IsYAMLPackageFile(filepath.Base(path)) {
		return declarationsForFile(filepath.Base(path), text)
	}
	packageDTO, matched, operationError := loading.LoadPackageFile(context.Background(), path)
	if operationError != nil || !matched {
		return nil
	}
	declarations := []namedDeclaration{}
	for _, declaration := range loading.PackageDeclarations(packageDTO) {
		start := position{}
		if offset := strings.Index(text, declaration.Name); offset >= 0 {
			start = positionForOffset(text, offset)
		}
		declarations = append(declarations, namedDeclaration{
			kind:       declaration.Kind,
			name:       declaration.Name,
			path:       path,
			rangeValue: rangeValue{Start: start, End: position{Line: start.Line, Character: start.Character + utf16Length(declaration.Name)}},
		})
	}
	return declarations
}

func declarationsForFile(fileName string, text string) []namedDeclaration {
	if isStarlarkFile(fileName) {
		return starlarkNamedDeclarations(text)
	}
	var root yaml.Node
	if operationError := yaml.Unmarshal([]byte(text), &root); operationError != nil {
		return incompleteYamlDeclarations(text)
	}
	if root.Kind == yaml.DocumentNode && len(root.Content) > 0 {
		root = *root.Content[0]
	}
	declarations := []namedDeclaration{}
	declarationKinds := yamlDeclarationKinds()
	if root.Kind != yaml.MappingNode {
		return declarations
	}
	for index := 0; index+1 < len(root.Content); index += 2 {
		key := root.Content[index]
		value := root.Content[index+1]
		kind, isDeclarationList := declarationKinds[key.Value]
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

func incompleteYamlDeclarations(text string) []namedDeclaration {
	namePattern := regexp.MustCompile(`^-\s+name:\s*["']?([^\s"'#]+)`)
	declarationKinds := yamlDeclarationKinds()
	declarations := []namedDeclaration{}
	kind := ""
	for lineNumber, line := range strings.Split(text, "\n") {
		trimmedLine := strings.TrimSpace(line)
		if collection, _, found := strings.Cut(trimmedLine, ":"); found {
			if declarationKind, isDeclarationList := declarationKinds[collection]; isDeclarationList {
				kind = declarationKind
				continue
			}
		}
		if kind == "" {
			continue
		}
		matches := namePattern.FindStringSubmatchIndex(trimmedLine)
		if len(matches) != 4 {
			continue
		}
		name := trimmedLine[matches[2]:matches[3]]
		byteStart := strings.Index(line, name)
		start := position{Line: lineNumber, Character: utf16Length(line[:byteStart])}
		end := position{Line: lineNumber, Character: start.Character + utf16Length(name)}
		declarations = append(declarations, namedDeclaration{kind: kind, name: name, rangeValue: rangeValue{Start: start, End: end}})
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
				workspaceRoot := findWorkspaceRoot(filepath.Dir(currentPath))
				return loading.ResolveStarlarkModulePath(workspaceRoot, currentPath, loadStatement.ModuleName()), loadStatement.From[index].Name, true
			}
		}
	}
	return "", "", false
}

func isStarlarkFile(fileName string) bool {
	return loading.IsStarlarkSourceFile(fileName)
}

func isSupportedBuildFile(fileName string) bool {
	return loading.IsPackageFile(fileName)
}

func watchedFileRegistrations() []map[string]any {
	patterns := []string{"**/grog*.toml"}
	for _, pattern := range loading.PackageFilePatterns() {
		patterns = append(patterns, "**/"+pattern)
	}
	for _, extension := range loading.StarlarkSourceExtensions() {
		patterns = append(patterns, "**/*"+extension)
	}
	registrations := make([]map[string]any, 0, len(patterns)+1)
	for _, pattern := range patterns {
		registrations = append(registrations, map[string]any{"globPattern": pattern, "kind": 7})
	}
	if environmentFilePath := config.Global.EnvironmentVariablesFile; environmentFilePath != "" {
		var pattern any = "**/" + filepath.ToSlash(environmentFilePath)
		if filepath.IsAbs(environmentFilePath) {
			pattern = map[string]any{"baseUri": pathToURI(filepath.Dir(environmentFilePath)), "pattern": filepath.Base(environmentFilePath)}
		}
		registrations = append(registrations, map[string]any{"globPattern": pattern, "kind": 7})
	}
	return registrations
}

func pathToURI(path string) string {
	return pathToURIForOperatingSystem(path, runtime.GOOS)
}

func pathToURIForOperatingSystem(path string, operatingSystem string) string {
	if operatingSystem == "windows" {
		path = strings.ReplaceAll(path, `\`, "/")
		if strings.HasPrefix(path, "//") {
			host, uriPath, _ := strings.Cut(strings.TrimPrefix(path, "//"), "/")
			return (&url.URL{Scheme: "file", Host: host, Path: "/" + uriPath}).String()
		}
		if len(path) >= 2 && path[1] == ':' {
			path = "/" + path
		}
	}
	return (&url.URL{Scheme: "file", Path: path}).String()
}

func starlarkIdentifierDefinitionRange(text string, identifier string) (rangeValue, bool) {
	file, operationError := syntax.Parse("", text, 0)
	if operationError == nil {
		for _, statement := range file.Stmts {
			var definition *syntax.Ident
			switch statement := statement.(type) {
			case *syntax.DefStmt:
				definition = statement.Name
			case *syntax.AssignStmt:
				definition, _ = statement.LHS.(*syntax.Ident)
			}
			if definition != nil && definition.Name == identifier {
				start, end := definition.Span()
				return rangeFromSyntaxPositions(text, start, end), true
			}
		}
		return rangeValue{}, false
	}
	patterns := []*regexp.Regexp{
		regexp.MustCompile(`^def\s+` + regexp.QuoteMeta(identifier) + `\s*\(`),
		regexp.MustCompile(`^` + regexp.QuoteMeta(identifier) + `\s*=`),
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
			startCharacter := utf16Length(line[:start])
			return rangeValue{Start: position{Line: lineNumber, Character: startCharacter}, End: position{Line: lineNumber, Character: startCharacter + utf16Length(identifier)}}, true
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
	return position{Line: lineNumber, Character: utf16Length(string(runes[:character]))}
}

func uriPath(documentURI string) string {
	return uriPathForOperatingSystem(documentURI, runtime.GOOS)
}

func uriPathForOperatingSystem(documentURI string, operatingSystem string) string {
	parsed, operationError := url.Parse(documentURI)
	if operationError != nil || parsed.Scheme != "file" {
		return documentURI
	}
	if operatingSystem == "windows" {
		path := parsed.Path
		if len(path) >= 3 && path[0] == '/' && path[2] == ':' {
			path = path[1:]
		}
		path = strings.ReplaceAll(path, "/", `\`)
		if parsed.Host != "" && parsed.Host != "localhost" {
			return `\\` + parsed.Host + path
		}
		return path
	}
	return parsed.Path
}
