package loading

import (
	"slices"
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

func TestBuildDeclarationSchemasUsePackageTags(t *testing.T) {
	want := []BuildDeclarationSchema{
		{Kind: "target", Collection: "targets"},
		{Kind: "alias", Collection: "aliases"},
		{Kind: "resource", Collection: "resources"},
		{Kind: "environment", Collection: "environments"},
	}
	if schemas := BuildDeclarationSchemas("yaml"); !slices.Equal(schemas, want) {
		t.Fatalf("schemas = %#v, want %#v", schemas, want)
	}
}
