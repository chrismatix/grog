package loading

import (
	"fmt"
	"reflect"
	"strings"

	"go.starlark.net/starlark"
)

// BuildDeclarationSchema identifies a declaration and its package collection.
type BuildDeclarationSchema struct {
	Kind       string
	Collection string
}

// StarlarkParameter describes an argument accepted by a declaration builtin.
type StarlarkParameter struct {
	Name     string
	Required bool
}

type buildDeclarationType struct {
	kind         string
	packageField string
	dataType     reflect.Type
}

const (
	targetDeclarationKind      = "target"
	aliasDeclarationKind       = "alias"
	resourceDeclarationKind    = "resource"
	environmentDeclarationKind = "environment"
)

var buildDeclarationTypes = []buildDeclarationType{
	{kind: targetDeclarationKind, packageField: "Targets", dataType: reflect.TypeFor[TargetDTO]()},
	{kind: aliasDeclarationKind, packageField: "Aliases", dataType: reflect.TypeFor[AliasDTO]()},
	{kind: resourceDeclarationKind, packageField: "Resources", dataType: reflect.TypeFor[ResourceDTO]()},
	{kind: environmentDeclarationKind, packageField: "Environments", dataType: reflect.TypeFor[EnvironmentDTO]()},
}

var starlarkParameters = map[string][]StarlarkParameter{
	targetDeclarationKind: {
		{Name: "name", Required: true},
		{Name: "command"},
		{Name: "dependencies"},
		{Name: "inputs"},
		{Name: "exclude_inputs"},
		{Name: "outputs"},
		{Name: "bin_output"},
		{Name: "binary_requires_push"},
		{Name: "output_checks"},
		{Name: "tags"},
		{Name: "fingerprint"},
		{Name: "platforms"},
		{Name: "environment_variables"},
		{Name: "timeout"},
		{Name: "concurrency_group"},
		{Name: "oci_push"},
	},
	aliasDeclarationKind: {
		{Name: "name", Required: true},
		{Name: "actual", Required: true},
	},
	resourceDeclarationKind: {
		{Name: "name", Required: true},
		{Name: "up", Required: true},
		{Name: "down"},
		{Name: "ready"},
		{Name: "timeout"},
		{Name: "exports"},
		{Name: "dependencies"},
	},
	environmentDeclarationKind: {
		{Name: "name", Required: true},
		{Name: "type", Required: true},
		{Name: "dependencies"},
		{Name: "oci_image"},
	},
}

// BuildDeclarationSchemas returns declaration names and package collections.
func BuildDeclarationSchemas(format string) []BuildDeclarationSchema {
	packageType := reflect.TypeFor[PackageDTO]()
	schemas := make([]BuildDeclarationSchema, 0, len(buildDeclarationTypes))
	for _, declarationType := range buildDeclarationTypes {
		packageField, found := packageType.FieldByName(declarationType.packageField)
		if !found {
			continue
		}
		collection := serializedFieldName(packageField, format)
		if collection != "" {
			schemas = append(schemas, BuildDeclarationSchema{Kind: declarationType.kind, Collection: collection})
		}
	}
	return schemas
}

// BuildFieldNames returns serialized package or declaration field names.
func BuildFieldNames(format string, declarationKind string) []string {
	dataType := reflect.TypeFor[PackageDTO]()
	if declarationKind != "" {
		dataType = nil
		for _, declarationType := range buildDeclarationTypes {
			if declarationType.kind == declarationKind {
				dataType = declarationType.dataType
				break
			}
		}
		if dataType == nil {
			return nil
		}
	}
	fields := make([]string, 0, dataType.NumField())
	for index := range dataType.NumField() {
		if name := serializedFieldName(dataType.Field(index), format); name != "" {
			fields = append(fields, name)
		}
	}
	return fields
}

// StarlarkParameters returns the arguments accepted by a declaration builtin.
func StarlarkParameters(declarationKind string) []StarlarkParameter {
	return append([]StarlarkParameter(nil), starlarkParameters[declarationKind]...)
}

func serializedFieldName(field reflect.StructField, format string) string {
	name := strings.Split(field.Tag.Get(format), ",")[0]
	if name == "-" {
		return ""
	}
	return name
}

func unpackStarlarkArgs(declarationKind string, arguments starlark.Tuple, keywordArguments []starlark.Tuple, destinations ...any) error {
	parameters := starlarkParameters[declarationKind]
	if len(parameters) != len(destinations) {
		return fmt.Errorf("internal %s schema has %d parameters for %d destinations", declarationKind, len(parameters), len(destinations))
	}
	unpackArguments := make([]any, 0, len(parameters)*2)
	for index, parameter := range parameters {
		name := parameter.Name
		if !parameter.Required {
			name += "?"
		}
		unpackArguments = append(unpackArguments, name, destinations[index])
	}
	return starlark.UnpackArgs(declarationKind, arguments, keywordArguments, unpackArguments...)
}
