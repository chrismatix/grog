package loading

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMavenDependencies(t *testing.T) {
	document, operationError := mavenDependencies(t.Context(), "testdata/maven")
	require.NoError(t, operationError)
	require.Equal(t, 1, document.Version)
	module := func(dependencies ...string) resolverPackage {
		return resolverPackage{Dependencies: append([]string{""}, dependencies...), Inputs: []string{"pom.xml", "src/**/*"}, ExcludeInputs: []string{"target/**"}}
	}
	require.Equal(t, map[string]resolverPackage{
		"":                 {Dependencies: []string{}, Inputs: []string{"pom.xml"}, ExcludeInputs: []string{"target/**"}},
		"core":             module(),
		"api":              module("core"),
		"services":         module(),
		"services/payment": module("api", "core", "services"),
		"services/billing": module("services/payment"),
		"tools/release":    module(),
	}, document.Packages)
}

func TestMavenRelativeDirectory(t *testing.T) {
	for _, testCase := range []struct {
		name          string
		fromDirectory string
		reference     string
		expected      string
		inside        bool
	}{
		{name: "module directory", fromDirectory: "", reference: "core", expected: "core", inside: true},
		{name: "nested module", fromDirectory: "services", reference: "payment", expected: "services/payment", inside: true},
		{name: "pom file", fromDirectory: "", reference: "tools/release/pom.xml", expected: "tools/release", inside: true},
		{name: "default parent", fromDirectory: "services/payment", reference: "../pom.xml", expected: "services", inside: true},
		{name: "parent is root", fromDirectory: "core", reference: "../pom.xml", expected: "", inside: true},
		{name: "outside workspace", fromDirectory: "", reference: "../outside", inside: false},
		{name: "parent above root", fromDirectory: "", reference: "../pom.xml", inside: false},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			directory, inside := mavenRelativeDirectory(testCase.fromDirectory, testCase.reference)
			require.Equal(t, testCase.inside, inside)
			require.Equal(t, testCase.expected, directory)
		})
	}
}

func TestMavenDefaultInputs(t *testing.T) {
	inputs, excludeInputs := mavenDefaultInputs("testdata/maven")
	require.Equal(t, []string{"api/pom.xml", "core/pom.xml", "pom.xml", "services/billing/pom.xml", "services/payment/pom.xml", "services/pom.xml", "tools/release/pom.xml"}, inputs)
	require.Equal(t, []string{"target/**"}, excludeInputs)
	inputs, excludeInputs = mavenDefaultInputs(t.TempDir())
	require.Equal(t, []string{"pom.xml"}, inputs)
	require.Equal(t, []string{"target/**"}, excludeInputs)
}

func TestMavenResolverErrors(t *testing.T) {
	_, operationError := mavenDependencies(t.Context(), t.TempDir())
	require.ErrorContains(t, operationError, "read")

	directory := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(directory, "pom.xml"), []byte("<project><modules><module>broken</module></modules></project>"), 0644))
	require.NoError(t, os.MkdirAll(filepath.Join(directory, "broken"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(directory, "broken/pom.xml"), []byte("<project>"), 0644))
	_, operationError = mavenDependencies(t.Context(), directory)
	require.ErrorContains(t, operationError, "parse")

	cancelledContext, cancel := context.WithCancel(t.Context())
	cancel()
	_, operationError = mavenDependencies(cancelledContext, "testdata/maven")
	require.ErrorIs(t, operationError, context.Canceled)
}
