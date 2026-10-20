package resolvers

import (
	"context"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMavenDependencies(t *testing.T) {
	document, operationError := mavenDependencies(t.Context(), "testdata/maven")
	require.NoError(t, operationError)
	require.Equal(t, 1, document.Version)
	module := func(dependencies ...string) Package {
		return Package{Dependencies: append([]string{}, dependencies...), Inputs: []string{"pom.xml", "src/**/*"}, ExcludeInputs: []string{"target/**"}}
	}
	require.Equal(t, map[string]Package{
		"":                   {Dependencies: []string{}, Inputs: []string{"pom.xml"}, ExcludeInputs: []string{"target/**"}},
		"bom":                module(""),
		"build-tools/plugin": module(""),
		"core":               module("", "bom"),
		"api":                module("", "build-tools/plugin", "core", "tools/release"),
		"services":           module(""),
		"services/payment":   module("api", "bom", "core", "services"),
		"services/billing":   module("", "build-tools/plugin", "services/payment"),
		"tools/release":      module(),
	}, document.Packages)
}

func TestMavenResolutionSemantics(t *testing.T) {
	for _, testCase := range []struct {
		name                 string
		files                map[string]string
		expectedDependencies map[string][]string
		expectedPackages     []string
	}{
		{
			name: "version selects one of duplicate coordinates",
			files: map[string]string{
				"pom.xml":     `<project><modules><module>v1</module><module>v2</module><module>app</module></modules></project>`,
				"v1/pom.xml":  `<project><groupId>com.example</groupId><artifactId>library</artifactId><version>1</version></project>`,
				"v2/pom.xml":  `<project><groupId>com.example</groupId><artifactId>library</artifactId><version>2</version></project>`,
				"app/pom.xml": `<project><groupId>com.example</groupId><artifactId>app</artifactId><version>1</version><dependencies><dependency><groupId>com.example</groupId><artifactId>library</artifactId><version>1</version></dependency></dependencies></project>`,
			},
			expectedDependencies: map[string][]string{"app": {"v1"}},
		},
		{
			name: "version range conservatively selects duplicate coordinates",
			files: map[string]string{
				"pom.xml":     `<project><modules><module>v1</module><module>v2</module><module>app</module></modules></project>`,
				"v1/pom.xml":  `<project><groupId>com.example</groupId><artifactId>library</artifactId><version>1</version></project>`,
				"v2/pom.xml":  `<project><groupId>com.example</groupId><artifactId>library</artifactId><version>2</version></project>`,
				"app/pom.xml": `<project><groupId>com.example</groupId><artifactId>app</artifactId><version>1</version><dependencies><dependency><groupId>com.example</groupId><artifactId>library</artifactId><version>[1,3)</version></dependency></dependencies></project>`,
			},
			expectedDependencies: map[string][]string{"app": {"v1", "v2"}},
		},
		{
			name: "properties come from the actual parent",
			files: map[string]string{
				"pom.xml":               `<project><groupId>com.example</groupId><artifactId>aggregator</artifactId><version>1</version><modules><module>actual-parent</module><module>core</module><module>apps/child</module></modules></project>`,
				"actual-parent/pom.xml": `<project><groupId>com.example</groupId><artifactId>parent</artifactId><version>1</version><properties><core.artifact>core</core.artifact></properties></project>`,
				"core/pom.xml":          `<project><groupId>com.example</groupId><artifactId>core</artifactId><version>1</version></project>`,
				"apps/child/pom.xml":    `<project><parent><groupId>com.example</groupId><artifactId>parent</artifactId><version>1</version><relativePath>../../actual-parent/pom.xml</relativePath></parent><artifactId>child</artifactId><dependencies><dependency><groupId>com.example</groupId><artifactId>${core.artifact}</artifactId><version>1</version></dependency></dependencies></project>`,
			},
			expectedDependencies: map[string][]string{"apps/child": {"actual-parent", "core"}},
		},
		{
			name: "relative parent must match declared coordinates",
			files: map[string]string{
				"pom.xml":            `<project><modules><module>apps</module><module>core</module></modules></project>`,
				"apps/pom.xml":       `<project><groupId>com.example</groupId><artifactId>aggregator</artifactId><version>1</version><properties><target.artifact>core</target.artifact></properties><modules><module>child</module></modules></project>`,
				"apps/child/pom.xml": `<project><parent><groupId>external.example</groupId><artifactId>parent</artifactId><version>1</version></parent><artifactId>child</artifactId><dependencies><dependency><groupId>com.example</groupId><artifactId>${target.artifact}</artifactId><version>1</version></dependency></dependencies></project>`,
				"core/pom.xml":       `<project><groupId>com.example</groupId><artifactId>core</artifactId><version>1</version></project>`,
			},
			expectedDependencies: map[string][]string{"apps/child": {}},
		},
		{
			name: "parent profile properties reach children",
			files: map[string]string{
				"pom.xml":        `<project><modules><module>parent</module><module>core</module><module>child</module></modules></project>`,
				"parent/pom.xml": `<project><groupId>com.example</groupId><artifactId>parent</artifactId><version>1</version><profiles><profile><properties><target.artifact>core</target.artifact></properties></profile></profiles></project>`,
				"core/pom.xml":   `<project><groupId>com.example</groupId><artifactId>core</artifactId><version>1</version></project>`,
				"child/pom.xml":  `<project><parent><groupId>com.example</groupId><artifactId>parent</artifactId><version>1</version><relativePath>../parent/pom.xml</relativePath></parent><artifactId>child</artifactId><dependencies><dependency><groupId>com.example</groupId><artifactId>${target.artifact}</artifactId><version>1</version></dependency></dependencies></project>`,
			},
			expectedDependencies: map[string][]string{"child": {"core", "parent"}},
		},
		{
			name: "local parent outside reactor remains a package",
			files: map[string]string{
				"pom.xml":              `<project><modules><module>child</module></modules></project>`,
				"parents/base/pom.xml": `<project><groupId>com.example</groupId><artifactId>parent</artifactId><version>1</version></project>`,
				"child/pom.xml":        `<project><parent><groupId>com.example</groupId><artifactId>parent</artifactId><version>1</version><relativePath>../parents/base/pom.xml</relativePath></parent><artifactId>child</artifactId></project>`,
			},
			expectedDependencies: map[string][]string{"child": {"parents/base"}},
			expectedPackages:     []string{"", "child", "parents/base"},
		},
		{
			name: "profiles keep separate property scopes",
			files: map[string]string{
				"pom.xml":       `<project><modules><module>lib-a</module><module>lib-b</module><module>app</module></modules></project>`,
				"lib-a/pom.xml": `<project><groupId>com.example</groupId><artifactId>lib-a</artifactId><version>1</version></project>`,
				"lib-b/pom.xml": `<project><groupId>com.example</groupId><artifactId>lib-b</artifactId><version>1</version></project>`,
				"app/pom.xml":   `<project><groupId>com.example</groupId><artifactId>app</artifactId><version>1</version><profiles><profile><properties><target.artifact>lib-a</target.artifact></properties><dependencies><dependency><groupId>com.example</groupId><artifactId>${target.artifact}</artifactId><version>1</version></dependency></dependencies></profile><profile><properties><target.artifact>lib-b</target.artifact></properties><dependencies><dependency><groupId>com.example</groupId><artifactId>${target.artifact}</artifactId><version>1</version></dependency></dependencies></profile></profiles></project>`,
			},
			expectedDependencies: map[string][]string{"app": {"lib-a", "lib-b"}},
		},
		{
			name: "chained properties resolve module paths",
			files: map[string]string{
				"pom.xml":             `<project><properties><base.dir>modules</base.dir><child.dir>${base.dir}/lib</child.dir></properties><modules><module>${child.dir}</module></modules></project>`,
				"modules/lib/pom.xml": `<project><groupId>com.example</groupId><artifactId>lib</artifactId><version>1</version></project>`,
			},
			expectedPackages: []string{"", "modules/lib"},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			directory := t.TempDir()
			for name, contents := range testCase.files {
				filePath := filepath.Join(directory, name)
				require.NoError(t, os.MkdirAll(filepath.Dir(filePath), 0755))
				require.NoError(t, os.WriteFile(filePath, []byte(contents), 0644))
			}
			document, operationError := mavenDependencies(t.Context(), directory)
			require.NoError(t, operationError)
			for packageDirectory, dependencies := range testCase.expectedDependencies {
				require.Equal(t, dependencies, document.Packages[packageDirectory].Dependencies)
			}
			if testCase.expectedPackages != nil {
				require.ElementsMatch(t, testCase.expectedPackages, slices.Collect(maps.Keys(document.Packages)))
			}
		})
	}
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
	require.Equal(t, []string{"api/pom.xml", "bom/pom.xml", "build-tools/plugin/pom.xml", "core/pom.xml", "pom.xml", "services/billing/pom.xml", "services/payment/pom.xml", "services/pom.xml", "tools/release/pom.xml"}, inputs)
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
	_, operationError = mavenMembers(cancelledContext, "testdata/maven")
	require.ErrorIs(t, operationError, context.Canceled)
}
