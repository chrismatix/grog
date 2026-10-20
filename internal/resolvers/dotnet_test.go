package resolvers

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func dotnetMember(dependencies ...string) Package {
	return Package{Dependencies: append([]string{}, dependencies...), Inputs: []string{"**/*"}, ExcludeInputs: dotnetExcludeInputs}
}

func TestDotnetDependencies(t *testing.T) {
	document, operationError := dotnetDependencies(t.Context(), "testdata/dotnet")
	require.NoError(t, operationError)
	require.Equal(t, 1, document.Version)
	importFingerprint := document.Packages["src/App"].Fingerprint["imports:App.csproj"]
	require.NotEmpty(t, importFingerprint)
	require.Equal(t, importFingerprint, document.Packages["src/Core"].Fingerprint["imports:Core.csproj"])
	for directory, reportedPackage := range document.Packages {
		reportedPackage.Fingerprint = nil
		document.Packages[directory] = reportedPackage
	}
	configuration := func(files ...string) Package {
		return Package{Dependencies: []string{}, Inputs: files, ExcludeInputs: dotnetExcludeInputs}
	}
	require.Equal(t, map[string]Package{
		"":                configuration("*.sln", "*.slnx", "Directory.Build.props"),
		"src":             configuration("Directory.Build.props"),
		"tests":           configuration("NuGet.Config"),
		"src/App":         dotnetMember("lib/Shared", "src", "src/Core", "src/Fs"),
		"src/Core":        dotnetMember("lib/Shared", "src"),
		"src/Fs":          dotnetMember("lib/Shared", "src"),
		"lib/Shared":      dotnetMember(""),
		"tests/App.Tests": dotnetMember("", "src/App", "tests"),
	}, document.Packages)
}

func writeDotnetFiles(t *testing.T, files map[string]string) string {
	t.Helper()
	directory := t.TempDir()
	for name, contents := range files {
		require.NoError(t, os.MkdirAll(filepath.Dir(filepath.Join(directory, name)), 0755))
		require.NoError(t, os.WriteFile(filepath.Join(directory, name), []byte(contents), 0644))
	}
	return directory
}

const dotnetEmptyProject = `<Project Sdk="Microsoft.NET.Sdk"></Project>`

func TestDotnetProjectFiles(t *testing.T) {
	for _, testCase := range []struct {
		name          string
		files         map[string]string
		expected      []string
		expectedError string
	}{
		{
			name: "no solution globs project files outside bin and obj",
			files: map[string]string{
				"src/A/A.csproj": dotnetEmptyProject, "src/B/B.vbproj": dotnetEmptyProject, "src/C/C.fsproj": dotnetEmptyProject,
				"src/A/bin/Debug/A.csproj": dotnetEmptyProject, "src/A/obj/A.csproj": dotnetEmptyProject,
			},
			expected: []string{"src/A/A.csproj", "src/B/B.vbproj", "src/C/C.fsproj"},
		},
		{
			name: "slnx with nested folders",
			files: map[string]string{
				"Demo.slnx":      `<Solution><Folder Name="/src/"><Project Path="src\A\A.csproj" /><Folder Name="/src/nested/"><Project Path="src/B/B.csproj" /></Folder></Folder><Project Path="../outside/O.csproj" /></Solution>`,
				"src/A/A.csproj": dotnetEmptyProject, "src/B/B.csproj": dotnetEmptyProject, "src/C/C.csproj": dotnetEmptyProject,
			},
			expected: []string{"src/A/A.csproj", "src/B/B.csproj"},
		},
		{
			name: "two solutions are merged",
			files: map[string]string{
				"One.sln": "Project(\"{FAE04EC0-301F-11D3-BF4B-00C04F79EFBC}\") = \"A\", \"src\\A\\A.csproj\", \"{1}\"\n",
				"Two.sln": "Project(\"{FAE04EC0-301F-11D3-BF4B-00C04F79EFBC}\") = \"A\", \"src\\A\\A.csproj\", \"{1}\"\nProject(\"{FAE04EC0-301F-11D3-BF4B-00C04F79EFBC}\") = \"B\", \"src\\B\\B.csproj\", \"{2}\"\n",
			},
			expected: []string{"src/A/A.csproj", "src/B/B.csproj"},
		},
		{name: "invalid slnx", files: map[string]string{"Demo.slnx": "<Solution>"}, expectedError: "parse"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			projectFiles, operationError := dotnetProjectFiles(t.Context(), writeDotnetFiles(t, testCase.files))
			if testCase.expectedError != "" {
				require.ErrorContains(t, operationError, testCase.expectedError)
				return
			}
			require.NoError(t, operationError)
			require.Equal(t, testCase.expected, projectFiles)
		})
	}
}

func TestDotnetCoLocatedProjectFingerprints(t *testing.T) {
	directory := writeDotnetFiles(t, map[string]string{
		"src/Directory.Build.props": `<Project />`,
		"src/A.csproj":              `<Project><Import Project="../eng/a.props" /></Project>`,
		"src/B.csproj":              dotnetEmptyProject,
		"eng/a.props":               `<Project />`,
	})
	document, operationError := dotnetDependencies(t.Context(), directory)
	require.NoError(t, operationError)
	require.NotEmpty(t, document.Packages["src"].Fingerprint["imports:A.csproj"])
}

func TestDotnetNestedProjectElements(t *testing.T) {
	directory := writeDotnetFiles(t, map[string]string{
		"src/App/App.csproj":   `<Project><Choose><When Condition="true"><Import Project="../../eng/common.props" /><ItemGroup><ProjectReference Include="../Core/Core.csproj" /></ItemGroup></When></Choose></Project>`,
		"src/Core/Core.csproj": dotnetEmptyProject,
		"eng/common.props":     `<Project />`,
	})
	document, operationError := dotnetDependencies(t.Context(), directory)
	require.NoError(t, operationError)
	require.Equal(t, []string{"src/Core"}, document.Packages["src/App"].Dependencies)
	require.NotEmpty(t, document.Packages["src/App"].Fingerprint["imports:App.csproj"])
}

func TestDotnetReferenceSources(t *testing.T) {
	directory := writeDotnetFiles(t, map[string]string{
		"src/Directory.Build.props":         `<Project><ItemGroup><ProjectReference Include="..\..\shared\Analyzers\Analyzers.csproj" /></ItemGroup></Project>`,
		"src/App/App.csproj":                `<Project><ItemGroup><ProjectReference Include="../../libs/**/*.csproj" /><ProjectReference Include="..\Plugin\Plugin.csproj" Condition="Exists('..\Plugin\Plugin.csproj')" /></ItemGroup></Project>`,
		"libs/One/One.csproj":               dotnetEmptyProject,
		"libs/nested/Two/Two.csproj":        dotnetEmptyProject,
		"shared/Analyzers/Analyzers.csproj": dotnetEmptyProject,
	})
	document, operationError := dotnetDependencies(t.Context(), directory)
	require.NoError(t, operationError)
	require.Equal(t, []string{"libs/One", "libs/nested/Two", "shared/Analyzers", "src"}, document.Packages["src/App"].Dependencies)
	require.Contains(t, document.Packages, "libs/nested/Two")
	require.NotContains(t, document.Packages, "src/Plugin")
}

func TestDotnetNearestConfiguration(t *testing.T) {
	directory := writeDotnetFiles(t, map[string]string{
		"Directory.Build.props":     `<Project />`,
		"App.csproj":                `<Project><ItemGroup><ProjectReference Include="src\Lib\Lib.csproj" /></ItemGroup></Project>`,
		"src/Directory.Build.props": `<Project />`,
		"src/Lib/Lib.csproj":        dotnetEmptyProject,
	})
	document, operationError := dotnetDependencies(t.Context(), directory)
	require.NoError(t, operationError)
	require.Equal(t, []string{"src/Lib"}, document.Packages[""].Dependencies)
	require.Equal(t, []string{"src"}, document.Packages["src/Lib"].Dependencies)
}

func TestDotnetDefaultInputs(t *testing.T) {
	inputs, excludeInputs := dotnetDefaultInputs("testdata/dotnet")
	require.Equal(t, []string{"*.sln", "*.slnx", "**/*.csproj", "**/*.fsproj", "**/*.vbproj", "**/Directory.Build.props", "**/Directory.Build.targets", "**/Directory.Packages.props", "**/global.json", "**/nuget.config", "**/NuGet.Config", "eng/common.props", "eng/more.xml"}, inputs)
	require.Equal(t, []string{"**/bin/**", "**/obj/**"}, excludeInputs)
	directory := writeDotnetFiles(t, map[string]string{"App.csproj": dotnetEmptyProject, "DIRECTORY.BUILD.PROPS": `<Project />`})
	inputs, _ = dotnetDefaultInputs(directory)
	require.NotContains(t, inputs, "DIRECTORY.BUILD.PROPS")
}

func TestDotnetResolverErrors(t *testing.T) {
	for _, testCase := range []struct {
		name          string
		files         map[string]string
		expectedError string
	}{
		{name: "invalid project xml", files: map[string]string{"src/A/A.csproj": "<Project>"}, expectedError: "parse"},
		{
			name: "project shares a directory with ancestor configuration",
			files: map[string]string{
				"Directory.Build.props": `<Project />`,
				"App.csproj":            `<Project><ItemGroup><ProjectReference Include="lib\Lib\Lib.csproj" /></ItemGroup></Project>`,
				"lib/Lib/Lib.csproj":    dotnetEmptyProject,
			},
			expectedError: `project package "" references descendant package "lib/Lib", creating a cycle through shared configuration`,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			_, operationError := dotnetDependencies(t.Context(), writeDotnetFiles(t, testCase.files))
			require.ErrorContains(t, operationError, testCase.expectedError)
		})
	}
	cancelledContext, cancel := context.WithCancel(t.Context())
	cancel()
	_, operationError := dotnetDependencies(cancelledContext, "testdata/dotnet")
	require.ErrorIs(t, operationError, context.Canceled)
	_, operationError = dotnetProjectFiles(cancelledContext, "testdata/dotnet")
	require.ErrorIs(t, operationError, context.Canceled)
}
