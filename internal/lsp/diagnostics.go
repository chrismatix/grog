package lsp

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"

	"grog/internal/loading"

	"github.com/pelletier/go-toml/v2"
	"github.com/subosito/gotenv"
	starlarkjson "go.starlark.net/lib/json"
	"go.starlark.net/lib/math"
	starlarktime "go.starlark.net/lib/time"
	"go.starlark.net/starlark"
	"go.starlark.net/syntax"
	"gopkg.in/yaml.v3"
)

func (server *server) publishDiagnostics(documentURI string) error {
	text := server.documentText(documentURI)
	diagnostics := diagnosticsFor(documentURI, text)
	return server.notify("textDocument/publishDiagnostics", map[string]any{"uri": documentURI, "diagnostics": diagnostics})
}

func diagnosticsFor(documentURI string, text string) []diagnostic {
	path := uriPath(documentURI)
	name := filepath.Base(path)
	if isStarlarkFile(name) {
		return starlarkDiagnostics(path, text)
	}
	if name == "BUILD.yaml" || name == "BUILD.yml" {
		return yamlDiagnostics(text)
	}
	return []diagnostic{}
}

func starlarkDiagnostics(path string, text string) []diagnostic {
	if _, operationError := syntax.Parse(path, text, syntax.RetainComments); operationError != nil {
		return []diagnostic{diagnosticFromError(text, operationError)}
	}
	thread := &starlark.Thread{
		Name: path,
		Load: loadModule(),
	}
	_, operationError := starlark.ExecFileOptions(&syntax.FileOptions{Set: true}, thread, path, text, starlarkPredeclared(path))
	diagnostics := starlarkSemanticDiagnostics(text)
	if operationError != nil && !isRepeatedKeywordArgumentError(operationError) {
		diagnostics = append([]diagnostic{diagnosticFromError(text, operationError)}, diagnostics...)
	}
	return diagnostics
}

func isRepeatedKeywordArgumentError(operationError error) bool {
	return strings.Contains(operationError.Error(), "keyword argument") && strings.Contains(operationError.Error(), "is repeated")
}

func targetBuiltin(thread *starlark.Thread, function *starlark.Builtin, arguments starlark.Tuple, keywordArguments []starlark.Tuple) (starlark.Value, error) {
	var name string
	var command string
	var dependencies, inputs, excludeInputs, outputs, outputChecks, tags, platforms *starlark.List
	var fingerprint, environmentVariables, ociPush *starlark.Dict
	var binOutput, timeout, concurrencyGroup string
	var binaryRequiresPush bool
	if operationError := starlark.UnpackArgs("target", arguments, keywordArguments, "name", &name, "command?", &command, "dependencies?", &dependencies, "inputs?", &inputs, "exclude_inputs?", &excludeInputs, "outputs?", &outputs, "bin_output?", &binOutput, "binary_requires_push?", &binaryRequiresPush, "output_checks?", &outputChecks, "tags?", &tags, "fingerprint?", &fingerprint, "platforms?", &platforms, "environment_variables?", &environmentVariables, "timeout?", &timeout, "concurrency_group?", &concurrencyGroup, "oci_push?", &ociPush); operationError != nil {
		return nil, operationError
	}
	return starlark.None, nil
}

func aliasBuiltin(thread *starlark.Thread, function *starlark.Builtin, arguments starlark.Tuple, keywordArguments []starlark.Tuple) (starlark.Value, error) {
	var name string
	var actual string
	if operationError := starlark.UnpackArgs("alias", arguments, keywordArguments, "name", &name, "actual", &actual); operationError != nil {
		return nil, operationError
	}
	return starlark.None, nil
}

func environmentBuiltin(thread *starlark.Thread, function *starlark.Builtin, arguments starlark.Tuple, keywordArguments []starlark.Tuple) (starlark.Value, error) {
	var name string
	var environmentType string
	var dependencies *starlark.List
	var ociImage string
	if operationError := starlark.UnpackArgs("environment", arguments, keywordArguments, "name", &name, "type", &environmentType, "dependencies?", &dependencies, "oci_image?", &ociImage); operationError != nil {
		return nil, operationError
	}
	return starlark.None, nil
}

func resourceBuiltin(thread *starlark.Thread, function *starlark.Builtin, arguments starlark.Tuple, keywordArguments []starlark.Tuple) (starlark.Value, error) {
	var name, up, down, ready, timeout string
	var exports *starlark.Dict
	var dependencies *starlark.List
	if operationError := starlark.UnpackArgs("resource", arguments, keywordArguments, "name", &name, "up", &up, "down?", &down, "ready?", &ready, "timeout?", &timeout, "exports?", &exports, "dependencies?", &dependencies); operationError != nil {
		return nil, operationError
	}
	return starlark.None, nil
}

func starlarkPredeclared(currentPath string) starlark.StringDict {
	workspaceRoot := findWorkspaceRoot(filepath.Dir(currentPath))
	platformTags := starlark.NewList(nil)
	platformTags.Freeze()
	predeclared := starlark.StringDict{
		"target":              starlark.NewBuiltin("target", targetBuiltin),
		"alias":               starlark.NewBuiltin("alias", aliasBuiltin),
		"resource":            starlark.NewBuiltin("resource", resourceBuiltin),
		"environment":         starlark.NewBuiltin("environment", environmentBuiltin),
		"json":                starlarkjson.Module,
		"math":                math.Module,
		"time":                starlarktime.Module,
		"GROG_OS":             starlark.String(runtime.GOOS),
		"GROG_ARCH":           starlark.String(runtime.GOARCH),
		"GROG_PLATFORM":       starlark.String(runtime.GOOS + "/" + runtime.GOARCH),
		"GROG_PLATFORM_TAGS":  platformTags,
		"GROG_ENV_FILE":       starlark.String(""),
		"GROG_WORKSPACE_ROOT": starlark.String(workspaceRoot),
		"GROG_GIT_HASH":       starlark.String(""),
	}
	configurationBytes, operationError := os.ReadFile(filepath.Join(workspaceRoot, "grog.toml"))
	if operationError != nil {
		return predeclared
	}
	var configuration struct {
		EnvironmentVariables     map[string]string `toml:"environment_variables"`
		EnvironmentVariablesFile string            `toml:"environment_variables_file"`
		OperatingSystem          string            `toml:"os"`
		Architecture             string            `toml:"arch"`
		PlatformTags             []string          `toml:"platform_tag"`
	}
	if toml.Unmarshal(configurationBytes, &configuration) != nil {
		return predeclared
	}
	operatingSystem := runtime.GOOS
	if configuration.OperatingSystem != "" {
		operatingSystem = configuration.OperatingSystem
	}
	architecture := runtime.GOARCH
	if configuration.Architecture != "" {
		architecture = configuration.Architecture
	}
	predeclared["GROG_OS"] = starlark.String(operatingSystem)
	predeclared["GROG_ARCH"] = starlark.String(architecture)
	predeclared["GROG_PLATFORM"] = starlark.String(operatingSystem + "/" + architecture)
	values := make([]starlark.Value, 0, len(configuration.PlatformTags))
	for _, tag := range configuration.PlatformTags {
		values = append(values, starlark.String(tag))
	}
	platformTags = starlark.NewList(values)
	platformTags.Freeze()
	predeclared["GROG_PLATFORM_TAGS"] = platformTags
	if configuration.EnvironmentVariablesFile != "" {
		environmentFilePath := configuration.EnvironmentVariablesFile
		if !filepath.IsAbs(environmentFilePath) {
			environmentFilePath = filepath.Join(workspaceRoot, environmentFilePath)
		}
		predeclared["GROG_ENV_FILE"] = starlark.String(environmentFilePath)
		if environmentVariables, readError := gotenv.Read(environmentFilePath); readError == nil {
			for name, value := range environmentVariables {
				predeclared[name] = starlark.String(value)
			}
		}
	}
	for name, value := range configuration.EnvironmentVariables {
		predeclared[name] = starlark.String(value)
	}
	return predeclared
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
	pattern := regexp.MustCompile(`(?s)\b(target|alias|resource|environment)\s*\([^)]*?\bname\s*=\s*["']([^"']+)["']`)
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
	return name == "target" || name == "alias" || name == "resource" || name == "environment"
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
	for index := 0; index+1 < len(root.Content); index += 2 {
		key := root.Content[index]
		value := root.Content[index+1]
		switch key.Value {
		case "targets", "aliases", "resources", "environments":
			kind := map[string]string{"targets": "target", "aliases": "alias", "resources": "resource", "environments": "environment"}[key.Value]
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
				declarations = append(declarations, namedDeclaration{kind: kind, name: nameNode.Value, rangeValue: yamlNodeRange(text, nameNode)})
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

func loadModule() func(thread *starlark.Thread, module string) (starlark.StringDict, error) {
	loaded := map[string]starlark.StringDict{}
	loadingModules := map[string]bool{}
	var load func(thread *starlark.Thread, module string) (starlark.StringDict, error)
	load = func(thread *starlark.Thread, module string) (starlark.StringDict, error) {
		modulePath := resolveStarlarkModulePath(thread.Name, module)
		if globals, isLoaded := loaded[modulePath]; isLoaded {
			return globals, nil
		}
		if loadingModules[modulePath] {
			return nil, fmt.Errorf("cycle detected while loading %q", module)
		}
		source, operationError := os.ReadFile(modulePath)
		if operationError != nil {
			return nil, fmt.Errorf("load %q: %w", module, operationError)
		}
		loadingModules[modulePath] = true
		defer delete(loadingModules, modulePath)
		moduleThread := &starlark.Thread{Name: modulePath, Load: load}
		globals, operationError := starlark.ExecFileOptions(&syntax.FileOptions{Set: true}, moduleThread, modulePath, source, starlarkPredeclared(modulePath))
		if operationError != nil {
			return nil, operationError
		}
		loaded[modulePath] = globals
		return globals, nil
	}
	return load
}

func yamlDiagnostics(text string) []diagnostic {
	var packageDTO loading.PackageDTO
	decoder := yaml.NewDecoder(strings.NewReader(text))
	decoder.KnownFields(true)
	if operationError := decoder.Decode(&packageDTO); operationError != nil {
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
