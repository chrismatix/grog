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
	resolverLabel := label.TL("", "custom")
	packages := []*model.Package{{DependencyResolvers: map[label.TargetLabel]*model.DependencyResolver{
		resolverLabel: {Label: resolverLabel, Command: command, Timeout: 2 * time.Second},
	}}}
	for _, packagePath := range []string{"app", "lib"} {
		targetLabel := label.TL(packagePath, "sources")
		packages = append(packages, &model.Package{Path: packagePath, Targets: map[label.TargetLabel]*model.Target{
			targetLabel: {Label: targetLabel, DependencyResolvers: []label.TargetLabel{resolverLabel}},
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
		{name: "unresolvable dependency", output: `{"version":1,"packages":{"app":{"dependencies":["missing"]}}}`, expectedError: "resolver //:custom reports app depends on missing, which has no target registered for //:custom"},
		{name: "missing version", output: `{"packages":{}}`, expectedError: "resolver //:custom must return version 1"},
		{name: "old version", output: `{"version":0,"packages":{}}`, expectedError: "must return version 1"},
		{name: "new version", output: `{"version":2,"packages":{}}`, expectedError: "resolver //:custom returned unsupported version 2; upgrade grog"},
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
					document := resolverDocument{Version: 1, Packages: map[string]resolverPackage{}}
					switch position {
					case "key":
						document.Packages[entry] = resolverPackage{}
					case "dependency":
						document.Packages["app"] = resolverPackage{Dependencies: []string{entry}}
					default:
						document.Packages["unused"] = resolverPackage{Dependencies: []string{entry}}
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
		{"nonzero exit", "printf 'resolver diagnostic\nsecond line' >&2; exit 7", false, "exit status 7\nresolver diagnostic\nsecond line"},
		{"timeout", "exec sleep 30", false, "after 20ms"},
		{"default shell flags", "false; printf '%s' '{\"version\":1,\"packages\":{}}'", false, "exit status 1"},
		{"disabled shell flags", "false; printf '%s' '{\"version\":1,\"packages\":{}}'", true, ""},
		{"missing tool", "grog_nonexistent_resolver_tool", false, "grog_nonexistent_resolver_tool"},
		{"stderr on success", "echo diagnostic >&2; printf '%s' '{\"version\":1,\"packages\":{}}'", false, ""},
		{"closed stdin", "if read value; then exit 1; fi; printf '%s' '{\"version\":1,\"packages\":{}}'", false, ""},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			packages := inferenceTestPackages(t, testCase.command)
			config.Global.DisableDefaultShellFlags = testCase.disableFlags
			if testCase.name == "timeout" {
				packages[0].DependencyResolvers[label.TL("", "custom")].Timeout = 20 * time.Millisecond
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
	for _, testCase := range []string{"unknown resolver", "duplicate registration", "unused resolver", "no resolvers", "duplicate reference"} {
		t.Run(testCase, func(t *testing.T) {
			packages := inferenceTestPackages(t, `printf '%s' '{"version":1,"packages":{}}'`)
			resolverLabel := label.TL("", "custom")
			target := packages[1].Targets[label.TL("app", "sources")]
			switch testCase {
			case "unknown resolver":
				target.DependencyResolvers = []label.TargetLabel{label.TL("", "missing")}
				require.EqualError(t, inferDependencies(t.Context(), packages), "no dependency resolver at //:missing (referenced by //app:sources); declared: //:custom")
			case "duplicate registration":
				otherLabel := label.TL("app", "build")
				packages[1].Targets[otherLabel] = &model.Target{Label: otherLabel, DependencyResolvers: []label.TargetLabel{resolverLabel}}
				require.EqualError(t, inferDependencies(t.Context(), packages), "resolver //:custom registered twice in package app: //app:build and //app:sources")
			case "unused resolver", "no resolvers":
				packages[0].DependencyResolvers[resolverLabel].Command = "touch invoked; exit 1"
				if testCase == "no resolvers" {
					packages[0].DependencyResolvers = nil
				}
				require.NoError(t, inferDependencies(t.Context(), packages[:1]))
				_, operationError := os.Stat(filepath.Join(config.Global.WorkspaceRoot, "invoked"))
				require.ErrorIs(t, operationError, os.ErrNotExist)
			case "duplicate reference":
				target.DependencyResolvers = append(target.DependencyResolvers, resolverLabel)
				require.NoError(t, inferDependencies(t.Context(), packages))
			}
		})
	}
}

func TestDependencyInferenceNestedRootAndEnvironment(t *testing.T) {
	packages := inferenceTestPackages(t, "")
	config.Global.EnvironmentVariables = map[string]string{"RESOLVER_SETTING": "configured"}
	t.Setenv("RESOLVER_INHERITED", "inherited")
	resolverLabel := label.TL("tools/rust", "custom")
	require.NoError(t, os.MkdirAll(filepath.Join(config.Global.WorkspaceRoot, "tools/rust"), 0755))
	packages[0].DependencyResolvers = map[label.TargetLabel]*model.DependencyResolver{resolverLabel: {Label: resolverLabel, Timeout: time.Second, Command: `
 test "$PWD" = "$GROG_WORKSPACE_ROOT/tools/rust"
 test "$GROG_PACKAGE" = tools/rust
 test "$GROG_RESOLVER_LABEL" = //tools/rust:custom
 test "$GROG_TARGET" = //tools/rust:custom
 test "$RESOLVER_SETTING" = configured
 test "$RESOLVER_INHERITED" = inherited
 printf '%s' '{"version":1,"packages":{"":{"dependencies":[".hidden"]}}}'
 `}}
	for index, packagePath := range []string{"tools/rust", "tools/rust/.hidden"} {
		targetLabel := label.TL(packagePath, "sources")
		packages[index+1].Targets = map[label.TargetLabel]*model.Target{targetLabel: {Label: targetLabel, DependencyResolvers: []label.TargetLabel{resolverLabel}}}
	}
	require.NoError(t, inferDependencies(t.Context(), packages))
	require.Equal(t, []label.TargetLabel{label.TL("tools/rust/.hidden", "sources")}, packages[1].Targets[label.TL("tools/rust", "sources")].Dependencies)
}

func TestDependencyInferenceConcurrentResolversMerge(t *testing.T) {
	packages := inferenceTestPackages(t, "")
	packages[0].DependencyResolvers = make(map[label.TargetLabel]*model.DependencyResolver)
	for _, name := range []string{"first", "second"} {
		resolverLabel := label.TL("", name)
		other := "first"
		if name == "first" {
			other = "second"
		}
		packages[0].DependencyResolvers[resolverLabel] = &model.DependencyResolver{Label: resolverLabel, Timeout: 3 * time.Second,
			Command: "echo invocation >> " + name + "; while [ ! -f " + other + " ]; do sleep 0.01; done; printf '%s' '{\"version\":1,\"packages\":{\"app\":{\"dependencies\":[\"lib\",\"//lib:sources\"]}}}'"}
		for _, loadedPackage := range packages[1:] {
			for _, target := range loadedPackage.Targets {
				target.DependencyResolvers = []label.TargetLabel{label.TL("", "first"), label.TL("", "second")}
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

func TestDependencyResolverMalformedOutputLimit(t *testing.T) {
	packages := inferenceTestPackages(t, "printf '%s' '"+strings.Repeat("x", 4096)+"'")
	operationError := inferDependencies(t.Context(), packages)
	require.ErrorContains(t, operationError, strings.Repeat("x", 2048))
	require.NotContains(t, operationError.Error(), strings.Repeat("x", 2049))
}

func TestLoadAllPackagesInfersDependencies(t *testing.T) {
	inferenceTestPackages(t, "")
	require.NoError(t, os.WriteFile(filepath.Join(config.Global.WorkspaceRoot, "BUILD.yaml"), []byte(`dependency_resolvers:
  - name: custom
    command: "printf '%s' '{\"version\":1,\"packages\":{\"\":{\"dependencies\":[\"//:explicit\"]}}}'"
targets:
  - name: sources
    dependency_resolvers: [":custom"]
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

func TestDependencyResolverStarlarkModule(t *testing.T) {
	inferenceTestPackages(t, "")
	require.NoError(t, os.WriteFile(filepath.Join(config.Global.WorkspaceRoot, "resolver.star"), []byte(`def declare_resolver():
    dependency_resolver(name="custom", command="true")
`), 0644))
	buildFile := filepath.Join(config.Global.WorkspaceRoot, "BUILD.star")
	require.NoError(t, os.WriteFile(buildFile, []byte(`load("resolver.star", "declare_resolver")
declare_resolver()
`), 0644))
	packageDTO, _, operationError := (StarlarkLoader{}).Load(t.Context(), buildFile)
	require.NoError(t, operationError)
	require.Len(t, packageDTO.DependencyResolvers, 1)
	require.Equal(t, "custom", packageDTO.DependencyResolvers[0].Name)
}
