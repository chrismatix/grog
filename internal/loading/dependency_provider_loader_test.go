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

func TestDependencyProviderLoaders(t *testing.T) {
	packageModule, operationError := filepath.Abs("../../pkl/package.pkl")
	require.NoError(t, operationError)
	testCases := []struct {
		name    string
		loader  Loader
		content string
	}{
		{"yaml", YamlLoader{}, `dependency_providers:
  - name: cargo
    command: builtin:cargo
    inputs: ["manifests/*.toml"]
    timeout: 2s
targets:
  - name: sources
    dependency_providers: [":cargo"]
`},
		{"json", JsonLoader{}, `{"dependency_providers":[{"name":"cargo","command":"builtin:cargo","inputs":["manifests/*.toml"],"timeout":"2s"}],"targets":[{"name":"sources","dependency_providers":[":cargo"]}]}`},
		{"star", StarlarkLoader{}, `dependency_provider(name="cargo", command="builtin:cargo", inputs=["manifests/*.toml"], timeout="2s")
target(name="sources", dependency_providers=[":cargo"])`},
		{"pkl", &PklLoader{}, fmt.Sprintf(`amends %q
dependency_providers { new { name = "cargo"; command = "builtin:cargo"; inputs { "manifests/*.toml" }; timeout = "2s" } }
targets { new { name = "sources"; dependency_providers { ":cargo" } } }
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
			require.Len(t, packageDTO.DependencyProviders, 1)
			require.Equal(t, &DependencyProviderDTO{Name: "cargo", Command: "builtin:cargo", Inputs: []string{"manifests/*.toml"}, Timeout: "2s"}, packageDTO.DependencyProviders[0])
			require.Equal(t, []string{":cargo"}, packageDTO.Targets[0].DependencyProviders)
			enrichedPackage, operationError := getEnrichedPackage(console.GetLogger(t.Context()), ".", packageDTO)
			require.NoError(t, operationError)
			require.Equal(t, []string{"manifests/member.toml"}, enrichedPackage.DependencyProviders[label.TL("", "cargo")].Inputs)
			require.Equal(t, 2*time.Second, enrichedPackage.DependencyProviders[label.TL("", "cargo")].Timeout)
			require.Equal(t, []label.TargetLabel{label.TL("", "cargo")}, enrichedPackage.Targets[label.TL("", "sources")].DependencyProviders)
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
					require.Equal(t, packageDTO.DependencyProviders, decoded.DependencyProviders)
					require.Equal(t, packageDTO.Targets[0].DependencyProviders, decoded.Targets[0].DependencyProviders)
				})
			}
		})
	}
}

func TestDependencyProviderEnrichment(t *testing.T) {
	for _, testCase := range []struct {
		name          string
		provider      DependencyProviderDTO
		expectedError string
	}{
		{"default timeout", DependencyProviderDTO{Name: "cargo", Command: "builtin:cargo"}, ""},
		{"missing name", DependencyProviderDTO{Command: "true"}, "target name is empty"},
		{"missing command", DependencyProviderDTO{Name: "cargo"}, "must define a command"},
		{"invalid timeout", DependencyProviderDTO{Name: "cargo", Command: "true", Timeout: "later"}, "failed to parse timeout for dependency provider //:cargo"},
		{"invalid glob", DependencyProviderDTO{Name: "cargo", Command: "true", Inputs: []string{"["}}, "failed to resolve inputs"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			enrichedPackage, operationError := getEnrichedPackage(console.GetLogger(t.Context()), "", PackageDTO{DependencyProviders: []*DependencyProviderDTO{&testCase.provider}})
			if testCase.expectedError != "" {
				require.ErrorContains(t, operationError, testCase.expectedError)
				return
			}
			require.NoError(t, operationError)
			provider := enrichedPackage.DependencyProviders[label.TL("", "cargo")]
			require.Equal(t, 60*time.Second, provider.Timeout)
			require.Contains(t, provider.Inputs, "Cargo.toml")
			require.Contains(t, provider.Inputs, "Cargo.lock")
		})
	}
}

func TestDependencyProviderDuplicateLabels(t *testing.T) {
	providerPackage := PackageDTO{SourceFilePath: "BUILD.star", DependencyProviders: []*DependencyProviderDTO{{Name: "same", Command: "true"}}}
	for _, testCase := range []struct {
		name  string
		other PackageDTO
	}{
		{"target", PackageDTO{Targets: []*TargetDTO{{Name: "same"}}}},
		{"alias", PackageDTO{Aliases: []*AliasDTO{{Name: "same", Actual: ":other"}}}},
		{"resource", PackageDTO{Resources: []*ResourceDTO{{Name: "same", Up: "true"}}}},
		{"provider", providerPackage},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			combined := testCase.other
			combined.DependencyProviders = append(append([]*DependencyProviderDTO{}, combined.DependencyProviders...), providerPackage.DependencyProviders...)
			_, operationError := getEnrichedPackage(console.GetLogger(t.Context()), "", combined)
			require.ErrorContains(t, operationError, "duplicate target label: same")
			for _, reverse := range []bool{false, true} {
				t.Run(fmt.Sprint(reverse), func(t *testing.T) {
					provider, operationError := getEnrichedPackage(console.GetLogger(t.Context()), "", providerPackage)
					require.NoError(t, operationError)
					other, operationError := getEnrichedPackage(console.GetLogger(t.Context()), "", testCase.other)
					require.NoError(t, operationError)
					if reverse {
						operationError = mergePackages(provider, other)
					} else {
						operationError = mergePackages(other, provider)
					}
					require.ErrorContains(t, operationError, "label: //:same")
				})
			}
		})
	}
}
