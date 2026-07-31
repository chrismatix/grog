package mcpserver

import (
	"context"
	"encoding/json"
	"slices"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type recordingRunner struct {
	arguments []string
	results   []json.RawMessage
}

func (runner *recordingRunner) run(_ context.Context, arguments ...string) ([]json.RawMessage, error) {
	runner.arguments = append([]string{}, arguments...)
	return runner.results, nil
}

func TestServerExposesQueryTools(t *testing.T) {
	contextValue := context.Background()
	runner := &recordingRunner{
		results: []json.RawMessage{json.RawMessage(`{"targets":["//app:test"]}`)},
	}
	server := newServer("test", runner)
	clientTransport, serverTransport := mcp.NewInMemoryTransports()

	serverSession, err := server.Connect(contextValue, serverTransport, nil)
	require.NoError(t, err)
	defer serverSession.Close()

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "test"}, nil)
	clientSession, err := client.Connect(contextValue, clientTransport, nil)
	require.NoError(t, err)
	defer clientSession.Close()

	toolsResult, err := clientSession.ListTools(contextValue, nil)
	require.NoError(t, err)
	require.Len(t, toolsResult.Tools, 7)

	toolNames := make([]string, 0, len(toolsResult.Tools))
	for _, tool := range toolsResult.Tools {
		toolNames = append(toolNames, tool.Name)
		require.NotNil(t, tool.Annotations)
		assert.True(t, tool.Annotations.ReadOnlyHint)
	}
	assert.True(t, slices.Contains(toolNames, "list_targets"))
	assert.True(t, slices.Contains(toolNames, "get_changed_targets"))
	assert.True(t, slices.Contains(toolNames, "get_dependency_graph"))

	callResult, err := clientSession.CallTool(contextValue, &mcp.CallToolParams{
		Name: "list_targets",
		Arguments: map[string]any{
			"patterns":    []string{"//app/..."},
			"target_type": "test",
		},
	})
	require.NoError(t, err)
	assert.False(t, callResult.IsError)
	assert.Equal(t, []string{"list", "//app/...", "--target-type=test"}, runner.arguments)

	structuredContent, ok := callResult.StructuredContent.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, []any{"//app:test"}, structuredContent["targets"])
}
