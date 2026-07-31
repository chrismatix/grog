package console

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/viper"
)

// JSONSchemaVersion identifies the stable CLI envelope schema.
const JSONSchemaVersion = "1"

const (
	// ErrorCodeInvalidInvocation identifies invalid command usage.
	ErrorCodeInvalidInvocation = "invalid_invocation"
	// ErrorCodeRuntimeFailure identifies configuration or operational failures.
	ErrorCodeRuntimeFailure = "runtime_failure"
	// ErrorCodeTargetFailure identifies failed build or test targets.
	ErrorCodeTargetFailure = "target_failure"
)

// JSONEnvelope wraps every structured CLI result and error.
type JSONEnvelope struct {
	SchemaVersion string `json:"schema_version"`
	Type          string `json:"type"`
	Data          any    `json:"data,omitempty"`
	Error         any    `json:"error,omitempty"`
}

// JSONError describes a stable machine-readable invocation error.
type JSONError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// JSONEnabled reports whether stable JSON output is active.
func JSONEnabled() bool {
	return viper.GetBool("json")
}

// ConfigureJSONFromArguments enables JSON before Cobra parses arguments.
func ConfigureJSONFromArguments(arguments []string) bool {
	for _, argument := range arguments {
		if argument == "--json" || strings.EqualFold(argument, "--json=true") {
			viper.Set("json", true)
			return true
		}
	}
	return false
}

// WriteResult emits one JSON result envelope.
func WriteResult(data any) {
	writeEnvelope(JSONEnvelope{
		SchemaVersion: JSONSchemaVersion,
		Type:          "result",
		Data:          data,
	})
}

// WriteText emits text directly or inside a JSON result envelope.
func WriteText(text string) {
	if JSONEnabled() {
		WriteResult(map[string]string{"text": text})
		return
	}
	fmt.Print(text)
}

// WriteError emits one JSON error envelope.
func WriteError(code string, message string) {
	writeEnvelope(JSONEnvelope{
		SchemaVersion: JSONSchemaVersion,
		Type:          "error",
		Error: JSONError{
			Code:    code,
			Message: message,
		},
	})
}

func writeEnvelope(envelope JSONEnvelope) {
	if encodeError := json.NewEncoder(os.Stdout).Encode(envelope); encodeError != nil {
		fmt.Fprintf(os.Stderr, "failed to encode JSON output: %v\n", encodeError)
	}
}
