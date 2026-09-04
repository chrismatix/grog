package lsp

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"grog/internal/loading"

	"go.starlark.net/starlark"
	"go.starlark.net/syntax"
	"gopkg.in/yaml.v3"
)

func (server *server) publishDiagnostics(documentURI string) error {
	text := server.documentText(documentURI)
	diagnostics := server.diagnosticsFor(documentURI, text)
	return server.notify("textDocument/publishDiagnostics", map[string]any{"uri": documentURI, "diagnostics": diagnostics})
}

func (server *server) publishDiagnosticsAfterChange(documentURI string) error {
	if operationError := server.publishDiagnostics(documentURI); operationError != nil {
		return operationError
	}
	fileName := filepath.Base(uriPath(documentURI))
	if !loading.IsStarlarkSourceFile(fileName) {
		return nil
	}
	return server.publishOpenStarlarkBuildDiagnostics(documentURI)
}

func (server *server) publishOpenStarlarkBuildDiagnostics(excludedDocumentURI string) error {
	for openDocumentURI := range server.documents {
		if openDocumentURI != excludedDocumentURI && loading.IsStarlarkPackageFile(filepath.Base(uriPath(openDocumentURI))) {
			if operationError := server.publishDiagnostics(openDocumentURI); operationError != nil {
				return operationError
			}
		}
	}
	return nil
}

func diagnosticsFor(documentURI string, text string) []diagnostic {
	return diagnosticsForReader(documentURI, text, func(path string) (string, error) {
		content, operationError := os.ReadFile(path)
		return string(content), operationError
	})
}

func (server *server) diagnosticsFor(documentURI string, text string) []diagnostic {
	diagnostics := diagnosticsForReader(documentURI, text, func(path string) (string, error) {
		if content, isOpen := server.documents[pathToURI(path)]; isOpen {
			return content, nil
		}
		content, operationError := os.ReadFile(path)
		return string(content), operationError
	})
	return append(diagnostics, server.siblingPackageDiagnostics(uriPath(documentURI), text)...)
}

func (server *server) siblingPackageDiagnostics(path string, text string) []diagnostic {
	if !loading.IsPackageFile(filepath.Base(path)) {
		return nil
	}
	packageLoader := loading.NewPackageLoader(nil)
	currentDeclarations := server.declarationsForPath(path, text)
	entries, operationError := os.ReadDir(filepath.Dir(path))
	if operationError != nil {
		return nil
	}
	type siblingDeclaration struct {
		kind     string
		fileName string
	}
	siblingDeclarations := map[string]siblingDeclaration{}
	workspaceRoot := findWorkspaceRoot(filepath.Dir(path))
	includeHidden := loading.WorkspaceIncludesHidden(workspaceRoot)
	for _, entry := range entries {
		siblingPath := filepath.Join(filepath.Dir(path), entry.Name())
		if entry.IsDir() || filepath.Clean(siblingPath) == filepath.Clean(path) || !loading.IsPackageFile(entry.Name()) {
			continue
		}
		if !includeHidden && isHiddenWorkspacePath(workspaceRoot, siblingPath) {
			continue
		}
		siblingText := server.documentText(pathToURI(siblingPath))
		var declarations []namedDeclaration
		if isStarlarkFile(entry.Name()) {
			declarations = server.declarationsForPath(siblingPath, siblingText)
		} else {
			declarations = packageFileDeclarations(siblingPath, siblingText, packageLoader)
		}
		for _, declaration := range declarations {
			if loading.IsBuildLabelKind(declaration.kind) {
				siblingDeclarations[declaration.name] = siblingDeclaration{kind: declaration.kind, fileName: entry.Name()}
			}
		}
	}
	diagnostics := []diagnostic{}
	reported := map[string]bool{}
	for _, declaration := range currentDeclarations {
		sibling, duplicate := siblingDeclarations[declaration.name]
		if !duplicate || !loading.IsBuildLabelKind(declaration.kind) || reported[declaration.name] {
			continue
		}
		diagnostics = append(diagnostics, diagnostic{Range: declaration.rangeValue, Severity: 1, Source: "grog", Message: fmt.Sprintf("duplicate declaration name %q; also declared as %s in %s", declaration.name, sibling.kind, sibling.fileName)})
		reported[declaration.name] = true
	}
	return diagnostics
}

func diagnosticsForReader(documentURI string, text string, readText func(path string) (string, error)) []diagnostic {
	path := uriPath(documentURI)
	name := filepath.Base(path)
	if isStarlarkFile(name) {
		return starlarkDiagnostics(path, text, readText)
	}
	if loading.IsYAMLPackageFile(name) {
		return yamlDiagnostics(path, text)
	}
	return []diagnostic{}
}

func starlarkDiagnostics(path string, text string, readText func(path string) (string, error)) []diagnostic {
	if _, operationError := syntax.Parse(path, text, syntax.RetainComments); operationError != nil {
		return []diagnostic{diagnosticFromError(text, operationError)}
	}
	declarations, operationError := evaluateStarlark(path, text, readText)
	for index, declaration := range declarations {
		if declaration.path != "" && filepath.Clean(declaration.path) != filepath.Clean(path) {
			loadedPath := declaration.path
			if directPath, found := starlarkDirectLoadPath(path, text, declaration.path, readText); found {
				loadedPath = directPath
			}
			if loadRange, found := starlarkLoadRange(path, text, loadedPath, ""); found {
				declarations[index].rangeValue = loadRange
			}
		}
	}
	diagnostics := duplicateNameDiagnostics(declarations)
	diagnostics = append(diagnostics, duplicateKeywordArgumentDiagnostics(text)...)
	if operationError != nil && !isRepeatedKeywordArgumentError(operationError) {
		diagnostics = append([]diagnostic{starlarkDiagnosticFromError(path, text, operationError)}, diagnostics...)
	}
	return diagnostics
}

func starlarkDirectLoadPath(buildPath string, text string, targetPath string, readText func(path string) (string, error)) (string, bool) {
	file, operationError := syntax.Parse(buildPath, text, 0)
	if operationError != nil {
		return "", false
	}
	workspaceRoot := findWorkspaceRoot(filepath.Dir(buildPath))
	for _, statement := range file.Stmts {
		loadStatement, isLoad := statement.(*syntax.LoadStmt)
		if !isLoad {
			continue
		}
		directPath := loading.ResolveStarlarkModulePath(workspaceRoot, buildPath, loadStatement.ModuleName())
		if filepath.Clean(directPath) == filepath.Clean(targetPath) || starlarkModuleLoadsPath(workspaceRoot, directPath, targetPath, readText, map[string]bool{}) {
			return directPath, true
		}
	}
	return "", false
}

func starlarkModuleLoadsPath(workspaceRoot string, modulePath string, targetPath string, readText func(path string) (string, error), visited map[string]bool) bool {
	modulePath = filepath.Clean(modulePath)
	if visited[modulePath] {
		return false
	}
	visited[modulePath] = true
	text, operationError := readText(modulePath)
	if operationError != nil {
		return false
	}
	file, operationError := syntax.Parse(modulePath, text, 0)
	if operationError != nil {
		return false
	}
	for _, statement := range file.Stmts {
		loadStatement, isLoad := statement.(*syntax.LoadStmt)
		if !isLoad {
			continue
		}
		loadedPath := loading.ResolveStarlarkModulePath(workspaceRoot, modulePath, loadStatement.ModuleName())
		if filepath.Clean(loadedPath) == filepath.Clean(targetPath) || starlarkModuleLoadsPath(workspaceRoot, loadedPath, targetPath, readText, visited) {
			return true
		}
	}
	return false
}

func starlarkDiagnosticFromError(path string, text string, operationError error) diagnostic {
	errorPath := starlarkErrorPath(operationError)
	if errorPath == "" || filepath.Clean(errorPath) == filepath.Clean(path) {
		return diagnosticFromError(text, operationError)
	}
	if loadRange, found := starlarkLoadRange(path, text, errorPath, operationError.Error()); found {
		return diagnostic{Range: loadRange, Severity: 1, Source: "grog", Message: operationError.Error()}
	}
	return diagnosticFromError(text, operationError)
}

func starlarkLoadRange(path string, text string, loadedPath string, errorMessage string) (rangeValue, bool) {
	file, parseError := syntax.Parse(path, text, 0)
	if parseError != nil {
		return rangeValue{}, false
	}
	var fallback *syntax.LoadStmt
	for _, statement := range file.Stmts {
		loadStatement, isLoad := statement.(*syntax.LoadStmt)
		if !isLoad {
			continue
		}
		if fallback == nil {
			fallback = loadStatement
		}
		modulePath := loading.ResolveStarlarkModulePath(findWorkspaceRoot(filepath.Dir(path)), path, loadStatement.ModuleName())
		if filepath.Clean(modulePath) == filepath.Clean(loadedPath) || errorMessage != "" && strings.Contains(errorMessage, loadStatement.ModuleName()) {
			start, end := loadStatement.Module.Span()
			return rangeFromSyntaxPositions(text, start, end), true
		}
	}
	if fallback != nil {
		start, end := fallback.Module.Span()
		return rangeFromSyntaxPositions(text, start, end), true
	}
	return rangeValue{}, false
}

func starlarkErrorPath(operationError error) string {
	var syntaxError syntax.Error
	if errors.As(operationError, &syntaxError) {
		return syntaxError.Pos.Filename()
	}
	var evaluationError *starlark.EvalError
	if errors.As(operationError, &evaluationError) && len(evaluationError.CallStack) > 0 {
		return evaluationError.CallStack[len(evaluationError.CallStack)-1].Pos.Filename()
	}
	return ""
}

func evaluateStarlark(path string, text string, readText func(path string) (string, error)) ([]namedDeclaration, error) {
	options := loading.StarlarkOptionsForWorkspace(findWorkspaceRoot(filepath.Dir(path)))
	return evaluateStarlarkWithOptions(path, text, readText, options)
}

func evaluateStarlarkWithOptions(path string, text string, readText func(path string) (string, error), options loading.StarlarkEvaluationOptions) ([]namedDeclaration, error) {
	declarations := []namedDeclaration{}
	options.ReadFile = func(path string) ([]byte, error) {
		content, operationError := readText(path)
		return []byte(content), operationError
	}
	options.DeclarationCallback = func(declaration loading.StarlarkDeclaration) {
		declarationRange := rangeValue{Start: position{Line: max(declaration.Line-1, 0)}, End: position{Line: max(declaration.Line-1, 0), Character: 1}}
		if sourceText, operationError := readText(declaration.Path); operationError == nil {
			start := positionFromOneBased(sourceText, declaration.Line, declaration.Column)
			declarationRange = rangeValue{Start: start, End: position{Line: start.Line, Character: start.Character + 1}}
		}
		declarations = append(declarations, namedDeclaration{kind: declaration.Kind, name: declaration.Name, path: declaration.Path, rangeValue: declarationRange})
	}
	packageDTO, operationError := loading.EvaluateStarlark(path, []byte(text), options)
	if operationError == nil {
		operationError = loading.ValidatePackageRequiredFields(packageDTO)
	}
	if operationError == nil {
		operationError = loading.ValidatePackageTimeouts(packageDTO)
	}
	if operationError == nil {
		operationError = loading.ValidatePackageOutputs(packageDTO)
	}
	if operationError == nil {
		packagePath, relativePathError := filepath.Rel(options.WorkspaceRoot, filepath.Dir(path))
		if relativePathError == nil {
			operationError = loading.ValidatePackageLabels(packageDTO, filepath.ToSlash(packagePath))
		}
	}
	return declarations, operationError
}

func isRepeatedKeywordArgumentError(operationError error) bool {
	return strings.Contains(operationError.Error(), "keyword argument") && strings.Contains(operationError.Error(), "is repeated")
}

type namedDeclaration struct {
	kind       string
	name       string
	path       string
	rangeValue rangeValue
}

func starlarkNamedDeclarations(text string) []namedDeclaration {
	file, operationError := syntax.Parse("", text, 0)
	if operationError == nil {
		declarations := []namedDeclaration{}
		for _, statement := range file.Stmts {
			expressionStatement, isExpression := statement.(*syntax.ExprStmt)
			if !isExpression {
				continue
			}
			call, isCall := expressionStatement.X.(*syntax.CallExpr)
			if !isCall {
				continue
			}
			function, isIdentifier := call.Fn.(*syntax.Ident)
			if !isIdentifier || !isDeclarationKind(function.Name) {
				continue
			}
			for _, argument := range call.Args {
				keyword, isKeyword := argument.(*syntax.BinaryExpr)
				if !isKeyword || keyword.Op != syntax.EQ {
					continue
				}
				keywordName, isName := keyword.X.(*syntax.Ident)
				nameLiteral, isLiteral := keyword.Y.(*syntax.Literal)
				if !isName || !isLiteral || keywordName.Name != "name" {
					continue
				}
				name, isString := nameLiteral.Value.(string)
				if isString {
					start, end := nameLiteral.Span()
					declarations = append(declarations, namedDeclaration{kind: function.Name, name: name, rangeValue: rangeFromSyntaxPositions(text, start, end)})
					break
				}
			}
		}
		return declarations
	}
	declarationKinds := loading.StarlarkBuiltinNames()
	for index, declarationKind := range declarationKinds {
		declarationKinds[index] = regexp.QuoteMeta(declarationKind)
	}
	pattern := regexp.MustCompile(`(?s)\b(` + strings.Join(declarationKinds, "|") + `)\s*\([^)]*?\bname\s*=\s*["']([^"']+)["']`)
	declarations := []namedDeclaration{}
	matches := pattern.FindAllStringSubmatchIndex(text, -1)
	for _, match := range matches {
		kind := text[match[2]:match[3]]
		name := text[match[4]:match[5]]
		start := positionForOffset(text, match[4])
		end := position{Line: start.Line, Character: start.Character + utf16Length(name)}
		declarations = append(declarations, namedDeclaration{kind: kind, name: name, rangeValue: rangeValue{Start: start, End: end}})
	}
	return declarations
}

func isDeclarationKind(name string) bool {
	return slices.Contains(loading.StarlarkBuiltinNames(), name)
}

func duplicateKeywordArgumentDiagnostics(text string) []diagnostic {
	file, operationError := syntax.Parse("", text, 0)
	if operationError != nil {
		return nil
	}
	diagnostics := []diagnostic{}
	syntax.Walk(file, func(node syntax.Node) bool {
		call, isCall := node.(*syntax.CallExpr)
		if !isCall {
			return true
		}
		seen := map[string]bool{}
		for _, argument := range call.Args {
			keyword, isKeyword := argument.(*syntax.BinaryExpr)
			if !isKeyword || keyword.Op != syntax.EQ {
				continue
			}
			identifier, isIdentifier := keyword.X.(*syntax.Ident)
			if !isIdentifier {
				continue
			}
			if seen[identifier.Name] {
				start, end := identifier.Span()
				diagnostics = append(diagnostics, diagnostic{Range: rangeFromSyntaxPositions(text, start, end), Severity: 1, Source: "grog", Message: fmt.Sprintf("duplicate keyword argument %q", identifier.Name)})
			}
			seen[identifier.Name] = true
		}
		return true
	})
	return diagnostics
}

func duplicateNameDiagnostics(declarations []namedDeclaration) []diagnostic {
	seen := map[string]namedDeclaration{}
	diagnostics := []diagnostic{}
	for _, declaration := range declarations {
		if !loading.IsBuildLabelKind(declaration.kind) {
			continue
		}
		previous, exists := seen[declaration.name]
		if !exists {
			seen[declaration.name] = declaration
			continue
		}
		diagnostics = append(diagnostics, diagnostic{Range: declaration.rangeValue, Severity: 1, Source: "grog", Message: fmt.Sprintf("duplicate declaration name %q; first declared as %s", declaration.name, previous.kind)})
	}
	return diagnostics
}

func yamlSemanticDiagnostics(text string, root *yaml.Node) []diagnostic {
	if root.Kind == yaml.DocumentNode && len(root.Content) > 0 {
		root = root.Content[0]
	}
	if root.Kind != yaml.MappingNode {
		return []diagnostic{}
	}
	diagnostics := []diagnostic{}
	declarations := []namedDeclaration{}
	declarationKinds := yamlDeclarationKinds()
	for index := 0; index+1 < len(root.Content); index += 2 {
		key := root.Content[index]
		value := root.Content[index+1]
		kind, isDeclarationList := declarationKinds[key.Value]
		if !isDeclarationList {
			continue
		}
		if value.Kind != yaml.SequenceNode {
			diagnostics = append(diagnostics, yamlNodeDiagnostic(text, value, fmt.Sprintf("%s must be a list", key.Value)))
			continue
		}
		for _, item := range value.Content {
			nameNode := yamlMappingValue(item, "name")
			if nameNode == nil || nameNode.Value == "" {
				diagnostics = append(diagnostics, yamlNodeDiagnostic(text, item, fmt.Sprintf("%s requires name", kind)))
				continue
			}
			for _, parameter := range loading.StarlarkParameters(kind) {
				if !parameter.Required || parameter.Name == "name" {
					continue
				}
				fieldNode := yamlMappingValue(item, parameter.Name)
				if fieldNode == nil || fieldNode.Value == "" {
					diagnostics = append(diagnostics, yamlNodeDiagnostic(text, item, fmt.Sprintf("%s requires %s", kind, parameter.Name)))
				}
			}
			declarations = append(declarations, namedDeclaration{kind: kind, name: nameNode.Value, rangeValue: yamlNodeRange(text, nameNode)})
		}
	}
	diagnostics = append(diagnostics, duplicateNameDiagnostics(declarations)...)
	return diagnostics
}

func yamlDeclarationKinds() map[string]string {
	kinds := make(map[string]string)
	for _, schema := range loading.BuildDeclarationSchemas("yaml") {
		kinds[schema.Collection] = schema.Kind
	}
	return kinds
}

func yamlMappingValue(node *yaml.Node, key string) *yaml.Node {
	return yamlMappingValueVisited(node, key, map[*yaml.Node]bool{})
}

func yamlMappingValueVisited(node *yaml.Node, key string, visited map[*yaml.Node]bool) *yaml.Node {
	if node == nil || visited[node] {
		return nil
	}
	visited[node] = true
	if node.Kind == yaml.AliasNode {
		return yamlMappingValueVisited(node.Alias, key, visited)
	}
	if node.Kind != yaml.MappingNode {
		return nil
	}
	for index := 0; index+1 < len(node.Content); index += 2 {
		if node.Content[index].Value == key && key != "<<" {
			return node.Content[index+1]
		}
	}
	for index := 0; index+1 < len(node.Content); index += 2 {
		if node.Content[index].Value != "<<" {
			continue
		}
		merged := node.Content[index+1]
		if merged.Kind == yaml.SequenceNode {
			for _, item := range merged.Content {
				if value := yamlMappingValueVisited(item, key, visited); value != nil {
					return value
				}
			}
			continue
		}
		if value := yamlMappingValueVisited(merged, key, visited); value != nil {
			return value
		}
	}
	return nil
}

func yamlNodeDiagnostic(text string, node *yaml.Node, message string) diagnostic {
	return diagnostic{Range: yamlNodeRange(text, node), Severity: 1, Source: "grog", Message: message}
}

func yamlNodeRange(text string, node *yaml.Node) rangeValue {
	line := 0
	character := 0
	width := 1
	if node != nil {
		line = node.Line - 1
		character = node.Column - 1
		width = utf16Length(node.Value)
		if width == 0 {
			width = 1
		}
	}
	if line < 0 {
		line = 0
	}
	if character < 0 {
		character = 0
	}
	start := positionFromOneBased(text, line+1, character+1)
	end := position{Line: start.Line, Character: start.Character + width}
	return rangeValue{Start: start, End: end}
}

func yamlDiagnostics(path string, text string) []diagnostic {
	packageDTO, operationError := loading.DecodeYAML([]byte(text))
	if operationError != nil {
		return []diagnostic{diagnosticFromError(text, operationError)}
	}
	if operationError := loading.ValidatePackageTimeouts(packageDTO); operationError != nil {
		return []diagnostic{diagnosticFromError(text, operationError)}
	}
	if operationError := loading.ValidatePackageOutputs(packageDTO); operationError != nil {
		return []diagnostic{diagnosticFromError(text, operationError)}
	}
	workspaceRoot := findWorkspaceRoot(filepath.Dir(path))
	packagePath, operationError := filepath.Rel(workspaceRoot, filepath.Dir(path))
	if operationError == nil {
		operationError = loading.ValidatePackageLabels(packageDTO, filepath.ToSlash(packagePath))
	}
	if operationError != nil {
		return []diagnostic{diagnosticFromError(text, operationError)}
	}
	var root yaml.Node
	if operationError := yaml.Unmarshal([]byte(text), &root); operationError != nil {
		return []diagnostic{diagnosticFromError(text, operationError)}
	}
	return yamlSemanticDiagnostics(text, &root)
}

func diagnosticFromError(text string, operationError error) diagnostic {
	line, character := 0, 0
	var syntaxError syntax.Error
	if errors.As(operationError, &syntaxError) {
		line = int(syntaxError.Pos.Line) - 1
		character = int(syntaxError.Pos.Col) - 1
	} else {
		positionPattern := regexp.MustCompile(`:(\d+):(\d+)`)
		matches := positionPattern.FindStringSubmatch(operationError.Error())
		if len(matches) == 3 {
			line, _ = strconv.Atoi(matches[1])
			character, _ = strconv.Atoi(matches[2])
			line--
			character--
		} else {
			linePattern := regexp.MustCompile(`line (\d+):`)
			matches = linePattern.FindStringSubmatch(operationError.Error())
			if len(matches) == 2 {
				line, _ = strconv.Atoi(matches[1])
				line--
			}
		}
	}
	if line < 0 {
		line = 0
	}
	if character < 0 {
		character = 0
	}
	start := positionFromOneBased(text, line+1, character+1)
	return diagnostic{Range: rangeValue{Start: start, End: position{Line: start.Line, Character: start.Character + 1}}, Severity: 1, Source: "grog", Message: operationError.Error()}
}
