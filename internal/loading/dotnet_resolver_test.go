package loading

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func dotnetMember(dependencies ...string) resolverPackage {
	return resolverPackage{Dependencies: append([]string{""}, dependencies...), Inputs: []string{"**/*"}, ExcludeInputs: dotnetExcludeInputs}
}

func TestDotnetDependencies(t *testing.T) {
	document, operationError := dotnetDependencies(t.Context(), "testdata/dotnet")
	require.NoError(t, operationError)
	require.Equal(t, 1, document.Version)
	require.Equal(t, map[string]resolverPackage{
		"":                {Dependencies: []string{}, Inputs: dotnetRootInputs, ExcludeInputs: dotnetExcludeInputs},
		"src/App":         dotnetMember("src/Core", "src/Fs"),
		"src/Core":        dotnetMember("lib/Shared"),
		"src/Fs":          dotnetMember(),
		"lib/Shared":      dotnetMember(),
		"tests/App.Tests": dotnetMember("src/App"),
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
			projectFiles, operationError := dotnetProjectFiles(writeDotnetFiles(t, testCase.files))
			if testCase.expectedError != "" {
				require.ErrorContains(t, operationError, testCase.expectedError)
				return
			}
			require.NoError(t, operationError)
			require.Equal(t, testCase.expected, projectFiles)
		})
	}
}

func TestDotnetRootProject(t *testing.T) {
	directory := writeDotnetFiles(t, map[string]string{
		"App.csproj":         `<Project><ItemGroup><ProjectReference Include="lib\Lib\Lib.csproj" /></ItemGroup></Project>`,
		"lib/Lib/Lib.csproj": dotnetEmptyProject,
	})
	document, operationError := dotnetDependencies(t.Context(), directory)
	require.NoError(t, operationError)
	require.Equal(t, map[string]resolverPackage{
		"":        {Dependencies: []string{"lib/Lib"}, Inputs: []string{"**/*"}, ExcludeInputs: dotnetExcludeInputs},
		"lib/Lib": dotnetMember(),
	}, document.Packages)
}

func TestDotnetDefaultInputs(t *testing.T) {
	inputs, excludeInputs := dotnetDefaultInputs("testdata/dotnet")
	require.Equal(t, []string{"*.sln", "*.slnx", "**/*.csproj", "**/*.fsproj", "**/*.vbproj"}, inputs)
	require.Equal(t, []string{"**/bin/**", "**/obj/**"}, excludeInputs)
}

func TestDotnetResolverErrors(t *testing.T) {
	for _, testCase := range []struct {
		name          string
		files         map[string]string
		expectedError string
	}{
		{name: "invalid project xml", files: map[string]string{"src/A/A.csproj": "<Project>"}, expectedError: "parse"},
		{
			name:          "reference to a missing project",
			files:         map[string]string{"src/A/A.csproj": `<Project><ItemGroup><ProjectReference Include="..\Gone\Gone.csproj" /></ItemGroup></Project>`},
			expectedError: "read",
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
}
