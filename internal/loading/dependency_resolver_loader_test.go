package loading

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"grog/internal/config"
	"grog/internal/console"
	"grog/internal/label"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestDependencyResolverLoaders(t *testing.T) {
	packageModule, operationError := filepath.Abs("../../pkl/package.pkl")
	require.NoError(t, operationError)
	testCases := []struct {
		name    string
		loader  Loader
		content string
	}{
		{"yaml", YamlLoader{}, `dependency_resolvers:
  - name: cargo
    command: builtin:cargo
    inputs: ["manifests/*.toml"]
    timeout: 2s
    synthesized_target: python
targets:
  - name: sources
    dependency_resolvers: [":cargo"]
`},
		{"json", JsonLoader{}, `{"dependency_resolvers":[{"name":"cargo","command":"builtin:cargo","inputs":["manifests/*.toml"],"timeout":"2s","synthesized_target":"python"}],"targets":[{"name":"sources","dependency_resolvers":[":cargo"]}]}`},
		{"star", StarlarkLoader{}, `dependency_resolver(name="cargo", command="builtin:cargo", inputs=["manifests/*.toml"], timeout="2s", synthesized_target="python")
target(name="sources", dependency_resolvers=[":cargo"])`},
		{"pkl", &PklLoader{}, fmt.Sprintf(`amends %q
dependency_resolvers { new { name = "cargo"; command = "builtin:cargo"; inputs { "manifests/*.toml" }; timeout = "2s"; synthesized_target = "python" } }
targets { new { name = "sources"; dependency_resolvers { ":cargo" } } }
`, packageModule)},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			originalConfig := config.Global
			t.Cleanup(func() { config.Global = originalConfig })
			directory := t.TempDir()
			config.Global.WorkspaceRoot = directory
			require.NoError(t, os.Mkdir(filepath.Join(directory, "manifests"), 0755))
			require.NoError(t, os.WriteFile(filepath.Join(directory, "manifests/member.toml"), nil, 0644))
			buildFile := filepath.Join(directory, "BUILD."+testCase.name)
			require.NoError(t, os.WriteFile(buildFile, []byte(testCase.content), 0644))
			packageDTO, matched, operationError := testCase.loader.Load(t.Context(), buildFile)
			require.NoError(t, operationError)
			require.True(t, matched)
			require.Len(t, packageDTO.DependencyResolvers, 1)
			require.Equal(t, &DependencyResolverDTO{Name: "cargo", Command: "builtin:cargo", Inputs: []string{"manifests/*.toml"}, Timeout: "2s", SynthesizedTarget: "python"}, packageDTO.DependencyResolvers[0])
			require.Equal(t, []string{":cargo"}, packageDTO.Targets[0].DependencyResolvers)
			enrichedPackage, operationError := getEnrichedPackage(console.GetLogger(t.Context()), ".", packageDTO)
			require.NoError(t, operationError)
			require.Equal(t, []string{"manifests/member.toml"}, enrichedPackage.DependencyResolvers[label.TL("", "cargo")].Inputs)
			require.Equal(t, 2*time.Second, enrichedPackage.DependencyResolvers[label.TL("", "cargo")].Timeout)
			require.Equal(t, "python", enrichedPackage.DependencyResolvers[label.TL("", "cargo")].SynthesizedTarget)
			require.Equal(t, []label.TargetLabel{label.TL("", "cargo")}, enrichedPackage.Targets[label.TL("", "sources")].DependencyResolvers)
			for _, format := range []string{"json", "yaml"} {
				t.Run(format+" round trip", func(t *testing.T) {
					var encoded []byte
					var decoded PackageDTO
					if format == "json" {
						encoded, operationError = json.Marshal(packageDTO)
						require.NoError(t, operationError)
						operationError = json.Unmarshal(encoded, &decoded)
					} else {
						encoded, operationError = yaml.Marshal(packageDTO)
						require.NoError(t, operationError)
						operationError = yaml.Unmarshal(encoded, &decoded)
					}
					require.NoError(t, operationError)
					require.Equal(t, packageDTO.DependencyResolvers, decoded.DependencyResolvers)
					require.Equal(t, packageDTO.Targets[0].DependencyResolvers, decoded.Targets[0].DependencyResolvers)
				})
			}
		})
	}
}

func TestDependencyResolverEnrichment(t *testing.T) {
	for _, testCase := range []struct {
		name          string
		resolver      DependencyResolverDTO
		expectedError string
	}{
		{"default timeout", DependencyResolverDTO{Name: "cargo", Command: "builtin:cargo"}, ""},
		{"missing name", DependencyResolverDTO{Command: "true"}, "target name is empty"},
		{"missing command", DependencyResolverDTO{Name: "cargo"}, "must define a command"},
		{"invalid timeout", DependencyResolverDTO{Name: "cargo", Command: "true", Timeout: "later"}, "failed to parse timeout for dependency resolver //:cargo"},
		{"invalid synthesized target", DependencyResolverDTO{Name: "cargo", Command: "true", SynthesizedTarget: "no spaces"}, "invalid synthesized_target for dependency resolver //:cargo"},
		{"invalid glob", DependencyResolverDTO{Name: "cargo", Command: "true", Inputs: []string{"["}}, "failed to resolve inputs"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			enrichedPackage, operationError := getEnrichedPackage(console.GetLogger(t.Context()), "", PackageDTO{DependencyResolvers: []*DependencyResolverDTO{&testCase.resolver}})
			if testCase.expectedError != "" {
				require.ErrorContains(t, operationError, testCase.expectedError)
				return
			}
			require.NoError(t, operationError)
			resolver := enrichedPackage.DependencyResolvers[label.TL("", "cargo")]
			require.Equal(t, 60*time.Second, resolver.Timeout)
			require.Contains(t, resolver.Inputs, "Cargo.toml")
			require.Contains(t, resolver.Inputs, "Cargo.lock")
		})
	}
}

func TestDependencyResolverDuplicateLabels(t *testing.T) {
	resolverPackage := PackageDTO{SourceFilePath: "BUILD.star", DependencyResolvers: []*DependencyResolverDTO{{Name: "same", Command: "true"}}}
	for _, testCase := range []struct {
		name  string
		other PackageDTO
	}{
		{"target", PackageDTO{Targets: []*TargetDTO{{Name: "same"}}}},
		{"alias", PackageDTO{Aliases: []*AliasDTO{{Name: "same", Actual: ":other"}}}},
		{"resource", PackageDTO{Resources: []*ResourceDTO{{Name: "same", Up: "true"}}}},
		{"resolver", resolverPackage},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			combined := testCase.other
			combined.DependencyResolvers = append(append([]*DependencyResolverDTO{}, combined.DependencyResolvers...), resolverPackage.DependencyResolvers...)
			_, operationError := getEnrichedPackage(console.GetLogger(t.Context()), "", combined)
			require.ErrorContains(t, operationError, "duplicate target label: same")
			for _, reverse := range []bool{false, true} {
				t.Run(fmt.Sprint(reverse), func(t *testing.T) {
					resolver, operationError := getEnrichedPackage(console.GetLogger(t.Context()), "", resolverPackage)
					require.NoError(t, operationError)
					other, operationError := getEnrichedPackage(console.GetLogger(t.Context()), "", testCase.other)
					require.NoError(t, operationError)
					if reverse {
						operationError = mergePackages(resolver, other)
					} else {
						operationError = mergePackages(other, resolver)
					}
					require.ErrorContains(t, operationError, "label: //:same")
				})
			}
		})
	}
}
