package cmds

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chrismatix/grog/internal/config"
	"github.com/chrismatix/grog/internal/label"
	"github.com/chrismatix/grog/internal/model"

	"github.com/stretchr/testify/require"
)

func TestResolverInputChanged(t *testing.T) {
	originalConfig := config.Global
	t.Cleanup(func() { config.Global = originalConfig })
	config.Global.WorkspaceRoot = t.TempDir()
	resolver := &model.DependencyResolver{Label: label.TL("tools", "cargo"), Inputs: []string{"Cargo.toml", "crates/a/Cargo.toml"}}
	for _, testCase := range []struct {
		name     string
		changed  string
		expected bool
	}{
		{"input under the declaring package", "tools/crates/a/Cargo.toml", true},
		{"same file name outside the declaring package", "crates/a/Cargo.toml", false},
		{"unrelated file", "tools/src/main.rs", false},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			changedFiles := []string{filepath.Join(config.Global.WorkspaceRoot, testCase.changed)}
			require.Equal(t, testCase.expected, resolverInputChanged(resolver, changedFiles))
		})
	}
}

func TestResolverTargets(t *testing.T) {
	resolverLabel := label.TL("", "cargo")
	registered := &model.Target{Label: label.TL("crates/a", "sources"), DependencyResolvers: []label.TargetLabel{resolverLabel}}
	synthesized := &model.Target{Label: label.TL("crates/b", "_cargo_package"), DependencyResolvers: []label.TargetLabel{resolverLabel}}
	other := &model.Target{Label: label.TL("crates/c", "build")}
	nodes := model.BuildNodeMapFromNodes(registered, synthesized, other)
	require.ElementsMatch(t, []*model.Target{registered, synthesized}, resolverTargets(nodes, resolverLabel))
}

const changesUvLock = `version = 1

[manifest]
members = ["a", "b", "c"]

[[package]]
name = "a"
version = "0.1.0"
source = { editable = "a" }
dependencies = [{ name = "requests" }]

[[package]]
name = "b"
version = "0.1.0"
source = { editable = "b" }
dependencies = [{ name = "urllib3" }]

[[package]]
name = "c"
version = "0.1.0"
source = { editable = "c" }
dependencies = [{ name = "numpy" }]

[[package]]
name = "requests"
version = "2.32.0"
source = { registry = "https://pypi.org/simple" }
dependencies = [{ name = "urllib3" }]

[[package]]
name = "urllib3"
version = "2.0.0"
source = { registry = "https://pypi.org/simple" }

[[package]]
name = "numpy"
version = "1.26.0"
source = { registry = "https://pypi.org/simple" }
`

func TestResolverChanges(t *testing.T) {
	for _, testCase := range []struct {
		name     string
		command  string
		old      string
		new      string
		expected map[string]string
	}{
		{
			name:     "dependency added to one member",
			command:  "builtin::uv",
			old:      `dependencies = [{ name = "requests" }]`,
			new:      `dependencies = [{ name = "requests" }, { name = "numpy" }]`,
			expected: map[string]string{"a": "fingerprint changed"},
		},
		{
			name:     "third-party bump marks the members reaching it",
			command:  "builtin::uv",
			old:      `version = "2.0.0"`,
			new:      `version = "2.0.1"`,
			expected: map[string]string{"a": "fingerprint changed", "b": "fingerprint changed"},
		},
		{
			name:     "edge added between members",
			command:  "builtin::uv",
			old:      `dependencies = [{ name = "numpy" }]`,
			new:      `dependencies = [{ name = "numpy" }, { name = "b" }]`,
			expected: map[string]string{"c": "edges changed"},
		},
		{
			name:     "edit that changes no report",
			command:  "builtin::uv",
			old:      "version = 1\n",
			new:      "version = 1\n# regenerated\n",
			expected: map[string]string{},
		},
		{
			name:     "command resolver keeps the blanket rule",
			command:  "cat uv.lock",
			old:      `version = "2.0.0"`,
			new:      `version = "2.0.1"`,
			expected: map[string]string{"a": "resolver input changed", "b": "resolver input changed", "c": "resolver input changed"},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			repository := newUvChangesRepository(t)
			lockPath := filepath.Join(repository, "uv.lock")
			require.NoError(t, os.WriteFile(lockPath, []byte(strings.Replace(changesUvLock, testCase.old, testCase.new, 1)), 0644))
			changedFiles, operationError := getChangedFiles("HEAD")
			require.NoError(t, operationError)
			require.Equal(t, []string{lockPath}, changedFiles)

			resolverLabel := label.TL("", "uv")
			resolver := &model.DependencyResolver{Label: resolverLabel, Command: testCase.command, Inputs: []string{"uv.lock", "a/pyproject.toml", "b/pyproject.toml", "c/pyproject.toml"}}
			var targets []model.BuildNode
			for _, member := range []string{"a", "b", "c"} {
				targets = append(targets, &model.Target{Label: label.TL(member, "_uv_package"), DependencyResolvers: []label.TargetLabel{resolverLabel}})
			}
			actual := make(map[string]string)
			for target, reason := range resolverChanges(t.Context(), model.BuildNodeMapFromNodes(targets...), resolver, changedFiles, "HEAD") {
				actual[target.Label.Package] = reason
			}
			require.Equal(t, testCase.expected, actual)
		})
	}

	t.Run("lock missing at the revision falls back to the blanket rule", func(t *testing.T) {
		repository := newUvChangesRepository(t)
		runGit(t, repository, "rm", "--quiet", "--cached", "uv.lock")
		runGit(t, repository, "commit", "--quiet", "-m", "untrack lock")
		require.NoError(t, os.WriteFile(filepath.Join(repository, "uv.lock"), []byte(changesUvLock+"\n"), 0644))
		changedFiles, operationError := getChangedFiles("HEAD")
		require.NoError(t, operationError)
		resolverLabel := label.TL("", "uv")
		resolver := &model.DependencyResolver{Label: resolverLabel, Command: "builtin::uv", Inputs: []string{"uv.lock"}}
		target := &model.Target{Label: label.TL("a", "_uv_package"), DependencyResolvers: []label.TargetLabel{resolverLabel}}
		reasons := resolverChanges(t.Context(), model.BuildNodeMapFromNodes(target), resolver, changedFiles, "HEAD")
		require.Equal(t, map[*model.Target]string{target: "resolver input changed"}, reasons)
	})
}

// newUvChangesRepository commits a three-member uv workspace and makes it the
// working directory and workspace root.
func newUvChangesRepository(t *testing.T) string {
	t.Helper()
	repository, operationError := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, operationError)
	originalConfig := config.Global
	t.Cleanup(func() { config.Global = originalConfig })
	config.Global.WorkspaceRoot = repository
	t.Chdir(repository)
	require.NoError(t, os.WriteFile(filepath.Join(repository, "uv.lock"), []byte(changesUvLock), 0644))
	for _, member := range []string{"a", "b", "c"} {
		require.NoError(t, os.MkdirAll(filepath.Join(repository, member), 0755))
		require.NoError(t, os.WriteFile(filepath.Join(repository, member, "pyproject.toml"), []byte("[project]\nname = \""+member+"\"\n"), 0644))
	}
	runGit(t, repository, "init", "--quiet")
	runGit(t, repository, "add", ".")
	runGit(t, repository, "commit", "--quiet", "-m", "base")
	return repository
}

func runGit(t *testing.T, directory string, arguments ...string) {
	t.Helper()
	command := exec.Command("git", append([]string{"-c", "user.name=grog", "-c", "user.email=grog@example.com"}, arguments...)...)
	command.Dir = directory
	output, operationError := command.CombinedOutput()
	require.NoError(t, operationError, string(output))
}
