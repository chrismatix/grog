package loading

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestUvDependencies(t *testing.T) {
	document, operationError := uvDependencies(t.Context(), "testdata/uv")
	require.NoError(t, operationError)
	require.Equal(t, 1, document.Version)
	excludeInputs := []string{"**/.venv/**", "**/__pycache__/**"}
	require.Equal(t, map[string]resolverPackage{
		"lib/format":   {Dependencies: []string{}, Inputs: []string{"format/**/*", "pyproject.toml"}, ExcludeInputs: excludeInputs},
		"lib/proto":    {Dependencies: []string{}, Inputs: []string{"**/*.py", "**/*.pyi", "pyproject.toml"}, ExcludeInputs: excludeInputs},
		"server":       {Dependencies: []string{"lib/format", "lib/proto", "tools/deploy"}, Inputs: []string{"pyproject.toml", "src/server/**/*"}, ExcludeInputs: excludeInputs},
		"cli":          {Dependencies: []string{"lib/format", "server"}, Inputs: []string{"**/*.py", "**/*.pyi", "cli/**/*", "pyproject.toml"}, ExcludeInputs: excludeInputs},
		"tools/deploy": {Dependencies: []string{}, Inputs: []string{"**/*.py", "**/*.pyi", "pyproject.toml"}, ExcludeInputs: excludeInputs},
		"lib/multi":    {Dependencies: []string{}, Inputs: []string{"pyproject.toml", "src/one/**/*", "src/two/inner/**/*"}, ExcludeInputs: excludeInputs},
	}, document.Packages)
	require.NotContains(t, document.Packages, "../external")
}

func TestUvDefaultInputs(t *testing.T) {
	inputs, excludeInputs := uvDefaultInputs("testdata/uv")
	require.Equal(t, []string{
		"cli/pyproject.toml", "lib/format/pyproject.toml", "lib/multi/pyproject.toml", "lib/proto/pyproject.toml",
		"pyproject.toml", "server/pyproject.toml", "tools/deploy/pyproject.toml", "uv.lock",
	}, inputs)
	require.Equal(t, []string{"**/.venv/**", "**/__pycache__/**"}, excludeInputs)
	inputs, _ = uvDefaultInputs(t.TempDir())
	require.Equal(t, []string{"uv.lock", "pyproject.toml"}, inputs)
}

func TestUvResolverErrors(t *testing.T) {
	directory := t.TempDir()
	_, operationError := uvDependencies(t.Context(), directory)
	require.ErrorContains(t, operationError, "read uv lock")
	require.NoError(t, os.WriteFile(filepath.Join(directory, "uv.lock"), []byte("[[package"), 0644))
	_, operationError = uvDependencies(t.Context(), directory)
	require.ErrorContains(t, operationError, "parse uv lock")
	cancelledContext, cancel := context.WithCancel(t.Context())
	cancel()
	_, operationError = uvDependencies(cancelledContext, "testdata/uv")
	require.ErrorIs(t, operationError, context.Canceled)
}
