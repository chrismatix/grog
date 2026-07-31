package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMCPServer(t *testing.T) {
	contextValue := context.Background()
	coverageDirectory := t.TempDir()
	command := exec.Command(binaryPath, "mcp")
	command.Dir = resolveRepoPath("querying")
	command.Env = append(
		os.Environ(),
		"GOCOVERDIR="+coverageDirectory,
		"GROG_ROOT="+filepath.Join(coverageDirectory, "grog_root"),
	)

	client := mcp.NewClient(&mcp.Implementation{Name: "integration-test", Version: "test"}, nil)
	clientSession, err := client.Connect(contextValue, &mcp.CommandTransport{Command: command}, nil)
	require.NoError(t, err)
	defer clientSession.Close()

	toolsResult, err := clientSession.ListTools(contextValue, nil)
	require.NoError(t, err)
	assert.Len(t, toolsResult.Tools, 7)

	callResult, err := clientSession.CallTool(contextValue, &mcp.CallToolParams{
		Name: "list_targets",
		Arguments: map[string]any{
			"patterns": []string{"//package_1/..."},
		},
	})
	require.NoError(t, err)
	require.False(t, callResult.IsError)

	structuredContent, ok := callResult.StructuredContent.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, []any{
		"//package_1:bar",
		"//package_1:foo",
		"//package_1:foo_test",
	}, structuredContent["targets"])
}
