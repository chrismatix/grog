package loading

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"grog/internal/analysis"
	"grog/internal/config"
	"grog/internal/label"
	"grog/internal/model"

	"github.com/stretchr/testify/require"
)

func inferenceTestPackages(t *testing.T, command string) []*model.Package {
	t.Helper()
	originalConfig := config.Global
	t.Cleanup(func() { config.Global = originalConfig })
	config.Global.WorkspaceRoot = t.TempDir()
	providerLabel := label.TL("", "custom")
	packages := []*model.Package{{DependencyProviders: map[label.TargetLabel]*model.DependencyProvider{
		providerLabel: {Label: providerLabel, Command: command, Timeout: 2 * time.Second},
	}}}
	for _, packagePath := range []string{"app", "lib"} {
		targetLabel := label.TL(packagePath, "sources")
		packages = append(packages, &model.Package{Path: packagePath, Targets: map[label.TargetLabel]*model.Target{
			targetLabel: {Label: targetLabel, DependencyProviders: []label.TargetLabel{providerLabel}},
		}})
	}
	return packages
}

func TestDependencyInferenceProtocol(t *testing.T) {
	for _, testCase := range []struct {
		name                 string
		output               string
		expectedError        string
		expectedDependencies []label.TargetLabel
	}{
		{name: "path dependency", output: `{"version":1,"packages":{"app":{"dependencies":["lib"]}}}`, expectedDependencies: []label.TargetLabel{label.TL("lib", "sources")}},
		{name: "literal label", output: `{"version":1,"packages":{"app":{"dependencies":["//lib:other"]}}}`, expectedDependencies: []label.TargetLabel{label.TL("lib", "other")}},
		{name: "self edges", output: `{"version":1,"packages":{"app":{"dependencies":["app","//app:sources"]}}}`},
		{name: "unknown fields", output: `{"version":1,"future":true,"packages":{"app":{"future":42}}}`},
		{name: "omitted packages", output: `{"version":1,"packages":{}}`},
		{name: "unregistered output ignored", output: `{"version":1,"packages":{"unused":{"dependencies":["missing"]}}}`},
		{name: "unresolvable dependency", output: `{"version":1,"packages":{"app":{"dependencies":["missing"]}}}`, expectedError: "provider //:custom reports app depends on missing, which has no target registered for //:custom"},
		{name: "missing version", output: `{"packages":{}}`, expectedError: "provider //:custom must return version 1"},
		{name: "old version", output: `{"version":0,"packages":{}}`, expectedError: "must return version 1"},
		{name: "new version", output: `{"version":2,"packages":{}}`, expectedError: "provider //:custom returned unsupported version 2; upgrade grog"},
		{name: "missing packages", output: `{"version":1}`, expectedError: "must return a packages object"},
		{name: "null package", output: `{"version":1,"packages":{"app":null}}`, expectedError: `package must be an object`},
		{name: "bare dependencies list", output: `{"version":1,"packages":{"app":["lib"]}}`, expectedError: "returned invalid JSON"},
		{name: "nonstring dependency", output: `{"version":1,"packages":{"app":{"dependencies":[1]}}}`, expectedError: "returned invalid JSON"},
		{name: "bad JSON", output: `diagnostic before JSON`, expectedError: `stdout: "diagnostic before JSON"`},
		{name: "multiple documents", output: `{"version":1,"packages":{}} {}`, expectedError: "returned invalid JSON"},
		{name: "invalid literal label", output: `{"version":1,"packages":{"app":{"dependencies":["//lib:"]}}}`, expectedError: `reports invalid dependency "//lib:"`},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			packages := inferenceTestPackages(t, "printf '%s' '"+testCase.output+"'")
			operationError := inferDependencies(t.Context(), packages)
			if testCase.expectedError != "" {
				require.ErrorContains(t, operationError, testCase.expectedError)
				return
			}
			require.NoError(t, operationError)
			require.Equal(t, testCase.expectedDependencies, packages[1].Targets[label.TL("app", "sources")].Dependencies)
		})
	}
}

func TestDependencyInferencePaths(t *testing.T) {
	for _, entry := range []string{"/absolute", "./leading", "trailing/", "../escape", "nested/../escape", "nested/..", `windows\path`, "C:/absolute", "//label:key"} {
		t.Run(entry, func(t *testing.T) {
			for _, position := range []string{"key", "dependency", "ignored package dependency"} {
				if position != "key" && strings.HasPrefix(entry, "//") {
					continue
				}
				t.Run(position, func(t *testing.T) {
					document := providerDocument{Version: 1, Packages: map[string]providerPackage{}}
					switch position {
					case "key":
						document.Packages[entry] = providerPackage{}
					case "dependency":
						document.Packages["app"] = providerPackage{Dependencies: []string{entry}}
					default:
						document.Packages["unused"] = providerPackage{Dependencies: []string{entry}}
					}
					output, operationError := json.Marshal(document)
					require.NoError(t, operationError)
					packages := inferenceTestPackages(t, "printf '%s' '"+string(output)+"'")
					require.ErrorContains(t, inferDependencies(t.Context(), packages), "invalid path")
				})
			}
		})
	}
}

func TestDependencyInferenceExecution(t *testing.T) {
	for _, testCase := range []struct {
		name          string
		command       string
		disableFlags  bool
		expectedError string
	}{
		{"nonzero exit", "printf 'provider diagnostic\nsecond line' >&2; exit 7", false, "exit status 7\nprovider diagnostic\nsecond line"},
		{"timeout", "exec sleep 30", false, "after 20ms"},
		{"default shell flags", "false; printf '%s' '{\"version\":1,\"packages\":{}}'", false, "exit status 1"},
		{"disabled shell flags", "false; printf '%s' '{\"version\":1,\"packages\":{}}'", true, ""},
		{"missing tool", "grog_nonexistent_provider_tool", false, "grog_nonexistent_provider_tool"},
		{"stderr on success", "echo diagnostic >&2; printf '%s' '{\"version\":1,\"packages\":{}}'", false, ""},
		{"closed stdin", "if read value; then exit 1; fi; printf '%s' '{\"version\":1,\"packages\":{}}'", false, ""},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			packages := inferenceTestPackages(t, testCase.command)
			config.Global.DisableDefaultShellFlags = testCase.disableFlags
			if testCase.name == "timeout" {
				packages[0].DependencyProviders[label.TL("", "custom")].Timeout = 20 * time.Millisecond
			}
			started := time.Now()
			operationError := inferDependencies(t.Context(), packages)
			if testCase.expectedError != "" {
				require.ErrorContains(t, operationError, testCase.expectedError)
			} else {
				require.NoError(t, operationError)
			}
			require.Less(t, time.Since(started), 3*time.Second)
		})
	}
}

func TestDependencyInferenceRegistrations(t *testing.T) {
	for _, testCase := range []string{"unknown provider", "duplicate registration", "unused provider", "no providers", "duplicate reference"} {
		t.Run(testCase, func(t *testing.T) {
			packages := inferenceTestPackages(t, `printf '%s' '{"version":1,"packages":{}}'`)
			providerLabel := label.TL("", "custom")
			target := packages[1].Targets[label.TL("app", "sources")]
			switch testCase {
			case "unknown provider":
				target.DependencyProviders = []label.TargetLabel{label.TL("", "missing")}
				require.EqualError(t, inferDependencies(t.Context(), packages), "no dependency provider at //:missing (referenced by //app:sources); declared: //:custom")
			case "duplicate registration":
				otherLabel := label.TL("app", "build")
				packages[1].Targets[otherLabel] = &model.Target{Label: otherLabel, DependencyProviders: []label.TargetLabel{providerLabel}}
				require.EqualError(t, inferDependencies(t.Context(), packages), "provider //:custom registered twice in package app: //app:build and //app:sources")
			case "unused provider", "no providers":
				packages[0].DependencyProviders[providerLabel].Command = "touch invoked; exit 1"
				if testCase == "no providers" {
					packages[0].DependencyProviders = nil
				}
				require.NoError(t, inferDependencies(t.Context(), packages[:1]))
				_, operationError := os.Stat(filepath.Join(config.Global.WorkspaceRoot, "invoked"))
				require.ErrorIs(t, operationError, os.ErrNotExist)
			case "duplicate reference":
				target.DependencyProviders = append(target.DependencyProviders, providerLabel)
				require.NoError(t, inferDependencies(t.Context(), packages))
			}
		})
	}
}

func TestDependencyInferenceNestedRootAndEnvironment(t *testing.T) {
	packages := inferenceTestPackages(t, "")
	config.Global.EnvironmentVariables = map[string]string{"PROVIDER_SETTING": "configured"}
	t.Setenv("PROVIDER_INHERITED", "inherited")
	providerLabel := label.TL("tools/rust", "custom")
	require.NoError(t, os.MkdirAll(filepath.Join(config.Global.WorkspaceRoot, "tools/rust"), 0755))
	packages[0].DependencyProviders = map[label.TargetLabel]*model.DependencyProvider{providerLabel: {Label: providerLabel, Timeout: time.Second, Command: `
 test "$PWD" = "$GROG_WORKSPACE_ROOT/tools/rust"
 test "$GROG_PACKAGE" = tools/rust
 test "$GROG_PROVIDER_LABEL" = //tools/rust:custom
 test "$GROG_TARGET" = //tools/rust:custom
 test "$PROVIDER_SETTING" = configured
 test "$PROVIDER_INHERITED" = inherited
 printf '%s' '{"version":1,"packages":{"":{"dependencies":[".hidden"]}}}'
 `}}
	for index, packagePath := range []string{"tools/rust", "tools/rust/.hidden"} {
		targetLabel := label.TL(packagePath, "sources")
		packages[index+1].Targets = map[label.TargetLabel]*model.Target{targetLabel: {Label: targetLabel, DependencyProviders: []label.TargetLabel{providerLabel}}}
	}
	require.NoError(t, inferDependencies(t.Context(), packages))
	require.Equal(t, []label.TargetLabel{label.TL("tools/rust/.hidden", "sources")}, packages[1].Targets[label.TL("tools/rust", "sources")].Dependencies)
}

func TestDependencyInferenceConcurrentProvidersMerge(t *testing.T) {
	packages := inferenceTestPackages(t, "")
	packages[0].DependencyProviders = make(map[label.TargetLabel]*model.DependencyProvider)
	for _, name := range []string{"first", "second"} {
		providerLabel := label.TL("", name)
		other := "first"
		if name == "first" {
			other = "second"
		}
		packages[0].DependencyProviders[providerLabel] = &model.DependencyProvider{Label: providerLabel, Timeout: 3 * time.Second,
			Command: "echo invocation >> " + name + "; while [ ! -f " + other + " ]; do sleep 0.01; done; printf '%s' '{\"version\":1,\"packages\":{\"app\":{\"dependencies\":[\"lib\",\"//lib:sources\"]}}}'"}
		for _, loadedPackage := range packages[1:] {
			for _, target := range loadedPackage.Targets {
				target.DependencyProviders = []label.TargetLabel{label.TL("", "first"), label.TL("", "second")}
			}
		}
	}
	target := packages[1].Targets[label.TL("app", "sources")]
	target.Dependencies = []label.TargetLabel{label.TL("z", "explicit"), label.TL("lib", "sources"), label.TL("a", "explicit")}
	require.NoError(t, inferDependencies(t.Context(), packages))
	require.Equal(t, []label.TargetLabel{label.TL("a", "explicit"), label.TL("lib", "sources"), label.TL("z", "explicit")}, target.Dependencies)
	for _, name := range []string{"first", "second"} {
		contents, operationError := os.ReadFile(filepath.Join(config.Global.WorkspaceRoot, name))
		require.NoError(t, operationError)
		require.Equal(t, "invocation\n", string(contents))
	}
}

func TestDependencyInferenceGraphErrors(t *testing.T) {
	for _, testCase := range []struct {
		name          string
		output        string
		expectedError string
	}{
		{"cycle", `{"version":1,"packages":{"app":{"dependencies":["lib"]},"lib":{"dependencies":["app"]}}}`, "cycle"},
		{"dangling literal", `{"version":1,"packages":{"app":{"dependencies":["//missing:target"]}}}`, "dependency //missing:target of node //app:sources not found"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			packages := inferenceTestPackages(t, "printf '%s' '"+testCase.output+"'")
			require.NoError(t, inferDependencies(t.Context(), packages))
			nodes, operationError := model.BuildNodeMapFromPackages(packages)
			require.NoError(t, operationError)
			_, operationError = analysis.BuildGraph(nodes)
			require.ErrorContains(t, operationError, testCase.expectedError)
		})
	}
}

func TestDependencyProviderMalformedOutputLimit(t *testing.T) {
	packages := inferenceTestPackages(t, "printf '%s' '"+strings.Repeat("x", 4096)+"'")
	operationError := inferDependencies(t.Context(), packages)
	require.ErrorContains(t, operationError, strings.Repeat("x", 2048))
	require.NotContains(t, operationError.Error(), strings.Repeat("x", 2049))
}

func TestLoadAllPackagesInfersDependencies(t *testing.T) {
	inferenceTestPackages(t, "")
	require.NoError(t, os.WriteFile(filepath.Join(config.Global.WorkspaceRoot, "BUILD.yaml"), []byte(`dependency_providers:
  - name: custom
    command: "printf '%s' '{\"version\":1,\"packages\":{\"\":{\"dependencies\":[\"//:explicit\"]}}}'"
targets:
  - name: sources
    dependency_providers: [":custom"]
  - name: explicit
`), 0644))
	loadedPackages, operationError := LoadAllPackages(t.Context())
	require.NoError(t, operationError)
	require.Len(t, loadedPackages, 1)
	require.Equal(t, []label.TargetLabel{label.TL("", "explicit")}, loadedPackages[0].Targets[label.TL("", "sources")].Dependencies)
}

func TestDependencyInferenceCancellation(t *testing.T) {
	packages := inferenceTestPackages(t, "exec sleep 30")
	loadContext, cancel := context.WithCancel(t.Context())
	cancel()
	require.ErrorIs(t, inferDependencies(loadContext, packages), context.Canceled)
}

func TestDependencyProviderStarlarkModule(t *testing.T) {
	inferenceTestPackages(t, "")
	require.NoError(t, os.WriteFile(filepath.Join(config.Global.WorkspaceRoot, "provider.star"), []byte(`def declare_provider():
    dependency_provider(name="custom", command="true")
`), 0644))
	buildFile := filepath.Join(config.Global.WorkspaceRoot, "BUILD.star")
	require.NoError(t, os.WriteFile(buildFile, []byte(`load("provider.star", "declare_provider")
declare_provider()
`), 0644))
	packageDTO, _, operationError := (StarlarkLoader{}).Load(t.Context(), buildFile)
	require.NoError(t, operationError)
	require.Len(t, packageDTO.DependencyProviders, 1)
	require.Equal(t, "custom", packageDTO.DependencyProviders[0].Name)
}
