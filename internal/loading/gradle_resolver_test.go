package loading

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGradleDependencies(t *testing.T) {
	document, operationError := gradleDependencies(t.Context(), "testdata/gradle")
	require.NoError(t, operationError)
	require.Equal(t, 1, document.Version)
	project := func(dependencies ...string) resolverPackage {
		return resolverPackage{
			Dependencies:  append([]string{""}, dependencies...),
			Inputs:        []string{"build.gradle", "build.gradle.kts", "gradle.properties", "src/**/*"},
			ExcludeInputs: []string{"build/**", ".gradle/**"},
		}
	}
	require.Equal(t, map[string]resolverPackage{
		"": {
			Dependencies:  []string{"build-logic"},
			Inputs:        []string{"settings.gradle", "settings.gradle.kts", "build.gradle", "build.gradle.kts", "gradle.properties", "gradle/**", "buildSrc/**"},
			ExcludeInputs: []string{"build/**", ".gradle/**", "buildSrc/build/**", "buildSrc/.gradle/**"},
		},
		"build-logic":       {Dependencies: []string{}, Inputs: []string{"**/*"}, ExcludeInputs: []string{"**/build/**", "**/.gradle/**"}},
		"app":               project("core-utils", "feature/moved-dir", "libs/api"),
		"core-utils":        project("libs/api"),
		"libs":              project(),
		"libs/api":          project(),
		"feature":           project(),
		"feature/moved-dir": project(),
		"unused":            project(),
		"services":          project(),
		"services/api":      project("services"),
	}, document.Packages)
}

func TestGradleIncludeBuildWithoutParentheses(t *testing.T) {
	directory := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(directory, "settings.gradle"), []byte("includeBuild 'build-logic'\ninclude ':app'\n"), 0644))
	document, operationError := gradleDependencies(t.Context(), directory)
	require.NoError(t, operationError)
	require.Equal(t, []string{"build-logic"}, document.Packages[""].Dependencies)
	require.Contains(t, document.Packages, "build-logic")
}

func TestGradleProjectDirectories(t *testing.T) {
	for _, testCase := range []struct {
		name        string
		settings    string
		directories []string
		expected    map[string]string
	}{
		{name: "groovy without parentheses", settings: "include ':a', ':b:c'\ninclude 'd'", expected: map[string]string{":a": "a", ":b:c": "b/c", ":d": "d"}},
		{name: "parent on disk is a project", settings: "include(\":b:c\")", directories: []string{"b/c"}, expected: map[string]string{":b": "b", ":b:c": "b/c"}},
		{name: "virtual parent is skipped", settings: "include(\":b:c\")\nproject(\":b:c\").projectDir = file(\"c\")", directories: []string{"c"}, expected: map[string]string{":b:c": "c"}},
		{name: "kotlin multiline", settings: "include(\n  \":a\",\n  \":b\"\n)", expected: map[string]string{":a": "a", ":b": "b"}},
		{name: "project dir override", settings: "include ':a'\nproject(':a').projectDir = new File(rootDir, 'lib/a')", expected: map[string]string{":a": "lib/a"}},
		{name: "override outside the workspace is dropped", settings: "include(\":a\")\nproject(\":a\").projectDir = file(\"../a\")", expected: map[string]string{}},
		{name: "includeBuild is not an include", settings: "includeBuild(\"logic\")", expected: map[string]string{}},
		{name: "root path is skipped", settings: "include(\":\")", expected: map[string]string{}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			workspaceDirectory := t.TempDir()
			for _, directory := range testCase.directories {
				require.NoError(t, os.MkdirAll(filepath.Join(workspaceDirectory, directory), 0755))
			}
			require.Equal(t, testCase.expected, gradleProjectDirectories(workspaceDirectory, testCase.settings))
		})
	}
}

func TestGradleAccessor(t *testing.T) {
	for projectPath, expected := range map[string]string{
		":core-utils":        "coreUtils",
		":libs:api":          "libs.api",
		":my_lib.v2":         "myLibV2",
		":feature:home-page": "feature.homePage",
	} {
		require.Equal(t, expected, gradleAccessor(projectPath))
	}
}

func TestGradleDefaultInputs(t *testing.T) {
	inputs, excludeInputs := gradleDefaultInputs("testdata/gradle")
	require.Equal(t, []string{
		"settings.gradle", "settings.gradle.kts",
		"app/build.gradle", "app/build.gradle.kts",
		"core-utils/build.gradle", "core-utils/build.gradle.kts",
		"feature/build.gradle", "feature/build.gradle.kts",
		"feature/moved-dir/build.gradle", "feature/moved-dir/build.gradle.kts",
		"libs/api/build.gradle", "libs/api/build.gradle.kts",
		"libs/build.gradle", "libs/build.gradle.kts",
		"services/api/build.gradle", "services/api/build.gradle.kts",
		"services/build.gradle", "services/build.gradle.kts",
		"unused/build.gradle", "unused/build.gradle.kts",
	}, inputs)
	require.Equal(t, []string{"build/**", ".gradle/**"}, excludeInputs)
	inputs, _ = gradleDefaultInputs(t.TempDir())
	require.Equal(t, []string{"settings.gradle", "settings.gradle.kts"}, inputs)
}

func TestGradleResolverErrors(t *testing.T) {
	_, operationError := gradleDependencies(t.Context(), t.TempDir())
	require.ErrorContains(t, operationError, "no settings.gradle")

	directory := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(directory, "settings.gradle"), []byte("include ':a'\n"), 0644))
	require.NoError(t, os.MkdirAll(filepath.Join(directory, "a", "build.gradle"), 0755))
	_, operationError = gradleDependencies(t.Context(), directory)
	require.ErrorContains(t, operationError, "read")

	cancelledContext, cancel := context.WithCancel(t.Context())
	cancel()
	_, operationError = gradleDependencies(cancelledContext, "testdata/gradle")
	require.ErrorIs(t, operationError, context.Canceled)
}
