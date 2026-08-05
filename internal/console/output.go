package console

import (
	"encoding/json"
	"errors"
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

type classifiedError struct {
	code  string
	cause error
}

func (classified *classifiedError) Error() string {
	return classified.cause.Error()
}

func (classified *classifiedError) Unwrap() error {
	return classified.cause
}

// JSONEnabled reports whether stable JSON output is active.
func JSONEnabled() bool {
	return viper.GetBool("json")
}

// ConfigureJSONFromArguments enables JSON before Cobra parses arguments.
func ConfigureJSONFromArguments(arguments []string) bool {
	for _, argument := range arguments {
		if argument == "--" {
			break
		}
		if argument == "--json" || strings.EqualFold(argument, "--json=true") {
			viper.Set("json", true)
			return true
		}
	}
	return false
}

// InvalidInvocation classifies an argument or command-usage error.
func InvalidInvocation(err error) error {
	return classifyError(ErrorCodeInvalidInvocation, err)
}

// RuntimeFailure classifies a configuration or operational error.
func RuntimeFailure(err error) error {
	return classifyError(ErrorCodeRuntimeFailure, err)
}

// ClassifyError returns the stable code and exit status for an error.
func ClassifyError(err error) (string, int) {
	var classified *classifiedError
	if errors.As(err, &classified) {
		return classified.code, exitStatus(classified.code)
	}
	return ErrorCodeInvalidInvocation, exitStatus(ErrorCodeInvalidInvocation)
}

func classifyError(code string, err error) error {
	var classified *classifiedError
	if errors.As(err, &classified) {
		return err
	}
	return &classifiedError{code: code, cause: err}
}

func exitStatus(code string) int {
	if code == ErrorCodeInvalidInvocation {
		return 2
	}
	return 1
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
