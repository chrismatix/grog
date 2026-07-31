package mcpserver

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseOutput(t *testing.T) {
	tests := []struct {
		name          string
		output        string
		expected      []json.RawMessage
		expectedError string
	}{
		{
			name: "collects results and ignores logs",
			output: `{"schema_version":"1","type":"log","level":"info","message":"loaded"}
{"schema_version":"1","type":"result","data":{"targets":["//:one"]}}
{"schema_version":"1","type":"result","data":{"targets":["//:two"]}}
`,
			expected: []json.RawMessage{
				json.RawMessage(`{"targets":["//:one"]}`),
				json.RawMessage(`{"targets":["//:two"]}`),
			},
		},
		{
			name:          "returns symbolic errors",
			output:        `{"schema_version":"1","type":"error","error":{"code":"runtime_failure","message":"broken"}}`,
			expectedError: "runtime_failure: broken",
		},
		{
			name:          "rejects invalid JSON",
			output:        "not-json",
			expectedError: "could not decode Grog JSON output",
		},
		{
			name:   "accepts empty output",
			output: "",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			results, err := parseOutput([]byte(test.output))
			if test.expectedError != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), test.expectedError)
				return
			}

			require.NoError(t, err)
			require.Len(t, results, len(test.expected))
			for resultIndex := range results {
				assert.JSONEq(t, string(test.expected[resultIndex]), string(results[resultIndex]))
			}
		})
	}
}
