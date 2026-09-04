package loading

import (
	"fmt"
	"reflect"
	"strings"

	"go.starlark.net/starlark"
)

// BuildDeclarationSchema identifies a declaration and its package collection.
type BuildDeclarationSchema struct {
	Kind        string
	Collection  string
	Addressable bool
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
	addressable  bool
}

const (
	targetDeclarationKind      = "target"
	aliasDeclarationKind       = "alias"
	resourceDeclarationKind    = "resource"
	environmentDeclarationKind = "environment"
)

var buildDeclarationTypes = []buildDeclarationType{
	{kind: targetDeclarationKind, packageField: "Targets", dataType: reflect.TypeFor[TargetDTO](), addressable: true},
	{kind: aliasDeclarationKind, packageField: "Aliases", dataType: reflect.TypeFor[AliasDTO](), addressable: true},
	{kind: resourceDeclarationKind, packageField: "Resources", dataType: reflect.TypeFor[ResourceDTO](), addressable: true},
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

// BuildFileNames returns every file name accepted by a package loader.
func BuildFileNames() []string {
	fileNames := make([]string, 0, len(starlarkBuildFileNames)+len(yamlBuildFileNames))
	fileNames = append(fileNames, starlarkBuildFileNames...)
	fileNames = append(fileNames, yamlBuildFileNames...)
	return fileNames
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
			schemas = append(schemas, BuildDeclarationSchema{Kind: declarationType.kind, Collection: collection, Addressable: declarationType.addressable})
		}
	}
	return schemas
}

// BuildFieldIsCollection reports whether a serialized declaration field is a slice.
func BuildFieldIsCollection(format string, declarationKind string, fieldName string) bool {
	for _, declarationType := range buildDeclarationTypes {
		if declarationType.kind != declarationKind {
			continue
		}
		for index := range declarationType.dataType.NumField() {
			field := declarationType.dataType.Field(index)
			if serializedFieldName(field, format) == fieldName {
				return field.Type.Kind() == reflect.Slice
			}
		}
	}
	return false
}

// IsBuildLabelKind reports whether a declaration becomes an addressable build node.
func IsBuildLabelKind(declarationKind string) bool {
	for _, declarationType := range buildDeclarationTypes {
		if declarationType.kind == declarationKind {
			return declarationType.addressable
		}
	}
	return false
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

// ValidatePackageRequiredFields validates non-empty values from the declaration schema.
func ValidatePackageRequiredFields(packageDTO PackageDTO) error {
	packageValue := reflect.ValueOf(packageDTO)
	for _, declarationType := range buildDeclarationTypes {
		declarations := packageValue.FieldByName(declarationType.packageField)
		for index := range declarations.Len() {
			declaration := declarations.Index(index)
			if declaration.IsNil() {
				continue
			}
			declaration = declaration.Elem()
			for _, parameter := range starlarkParameters[declarationType.kind] {
				if !parameter.Required {
					continue
				}
				for fieldIndex := range declaration.NumField() {
					fieldType := declaration.Type().Field(fieldIndex)
					if serializedFieldName(fieldType, "starlark") == parameter.Name && declaration.Field(fieldIndex).Kind() == reflect.String && declaration.Field(fieldIndex).String() == "" {
						return fmt.Errorf("%s requires %s", declarationType.kind, parameter.Name)
					}
				}
			}
		}
	}
	return nil
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
