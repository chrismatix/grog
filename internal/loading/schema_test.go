package loading

import (
	"slices"
	"strings"
	"testing"
)

func TestStarlarkSchemaMatchesLoaderDeclarations(t *testing.T) {
	declarationKinds := make([]string, 0, len(buildDeclarationTypes))
	for _, schema := range BuildDeclarationSchemas("starlark") {
		declarationKinds = append(declarationKinds, schema.Kind)
		fields := BuildFieldNames("starlark", schema.Kind)
		parameters := StarlarkParameters(schema.Kind)
		parameterNames := make([]string, 0, len(parameters))
		for _, parameter := range parameters {
			parameterNames = append(parameterNames, parameter.Name)
		}
		slices.Sort(fields)
		slices.Sort(parameterNames)
		if !slices.Equal(fields, parameterNames) {
			t.Errorf("%s fields = %v, parameters = %v", schema.Kind, fields, parameterNames)
		}
	}
	slices.Sort(declarationKinds)
	if builtins := StarlarkBuiltinNames(); !slices.Equal(declarationKinds, builtins) {
		t.Fatalf("declaration schemas = %v, builtins = %v", declarationKinds, builtins)
	}
}

func TestPackageFilePatternsMatchLoaderRegistry(t *testing.T) {
	for _, pattern := range PackageFilePatterns() {
		fileName := strings.Replace(pattern, "*", "sample", 1)
		if !IsPackageFile(fileName) {
			t.Errorf("pattern %q does not match %q", pattern, fileName)
		}
	}
}

func TestBuildDeclarationSchemasUsePackageTags(t *testing.T) {
	want := []BuildDeclarationSchema{
		{Kind: "target", Collection: "targets", Addressable: true},
		{Kind: "alias", Collection: "aliases", Addressable: true},
		{Kind: "resource", Collection: "resources", Addressable: true},
		{Kind: "environment", Collection: "environments"},
	}
	if schemas := BuildDeclarationSchemas("yaml"); !slices.Equal(schemas, want) {
		t.Fatalf("schemas = %#v, want %#v", schemas, want)
	}
	if !BuildFieldIsCollection("starlark", targetDeclarationKind, "dependencies") || BuildFieldIsCollection("starlark", aliasDeclarationKind, "actual") {
		t.Fatal("unexpected collection metadata")
	}
	if !IsBuildLabelKind(resourceDeclarationKind) || IsBuildLabelKind(environmentDeclarationKind) {
		t.Fatal("unexpected addressable declaration metadata")
	}
	if !slices.Contains(BuildFileNames(), "BUILD.star") || !slices.Contains(BuildFileNames(), "BUILD.yaml") {
		t.Fatalf("unexpected build file names: %v", BuildFileNames())
	}
}
