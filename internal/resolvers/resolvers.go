// Package resolvers holds the dependency resolver protocol document and the
// built-in resolvers that produce it without shelling out.
package resolvers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

// Document is version 1 of the output a dependency resolver prints.
type Document struct {
	Version  int                `json:"version"`
	Packages map[string]Package `json:"packages"`
}

// Package is one directory a resolver reports, relative to its declaring package.
type Package struct {
	Dependencies []string `json:"dependencies"`
	// Inputs let the resolver stand in for a package that registers no target:
	// grog synthesizes a filegroup from them. Ignored when a target registers.
	Inputs        []string `json:"inputs"`
	ExcludeInputs []string `json:"exclude_inputs"`
}

// UnmarshalJSON requires each package entry to be an object.
func (reportedPackage *Package) UnmarshalJSON(contents []byte) error {
	if bytes.Equal(bytes.TrimSpace(contents), []byte("null")) {
		return fmt.Errorf("package must be an object")
	}
	type packageFields Package
	return json.Unmarshal(contents, (*packageFields)(reportedPackage))
}

// Builtin is a resolver shipped with grog, selected by its builtin:: command.
type Builtin struct {
	Resolve func(resolverContext context.Context, workspaceDirectory string) (Document, error)
	// DefaultInputs are used when a declaration lists no inputs of its own.
	DefaultInputs func(workspaceDirectory string) (inputs []string, excludeInputs []string)
}

// Builtins maps each builtin:: command to its resolver.
var Builtins = map[string]Builtin{
	"builtin::aube":   {aubePackageManager.dependencies, aubePackageManager.defaultInputs},
	"builtin::cargo":  {cargoDependencies, cargoDefaultInputs},
	"builtin::go":     {goDependencies, goDefaultInputs},
	"builtin::gradle": {gradleDependencies, gradleDefaultInputs},
	"builtin::maven":  {mavenDependencies, mavenDefaultInputs},
	"builtin::npm":    {npmPackageManager.dependencies, npmPackageManager.defaultInputs},
	"builtin::pnpm":   {pnpmPackageManager.dependencies, pnpmPackageManager.defaultInputs},
	"builtin::uv":     {uvDependencies, uvDefaultInputs},
	"builtin::yarn":   {npmPackageManager.dependencies, npmPackageManager.defaultInputs},
}

// ValidatePath checks a path a resolver reports against the protocol.
func ValidatePath(entry string) error {
	if strings.HasPrefix(entry, "/") || strings.HasPrefix(entry, "./") || strings.HasSuffix(entry, "/") || strings.Contains(entry, "\\") || (len(entry) > 1 && entry[1] == ':') || slices.Contains(strings.Split(entry, "/"), "..") {
		return fmt.Errorf("invalid path %q: paths must be relative, slash-separated, without a leading ./, trailing /, or .. segment", entry)
	}
	return nil
}
