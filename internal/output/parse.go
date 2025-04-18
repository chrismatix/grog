package output

import (
	"fmt"
	"grog/internal/model"
	"grog/internal/output/handlers"
	"strings"
)

func ParseOutputs(outputs []string) ([]model.Output, error) {
	var parsedOutputs []model.Output
	for _, outputDefinition := range outputs {
		parsedOutput, err := ParseOutput(outputDefinition)
		if err != nil {
			return nil, err
		}
		parsedOutputs = append(parsedOutputs, parsedOutput)
	}
	return parsedOutputs, nil
}

// ParseOutput takes a raw output string and determines its type and reference
func ParseOutput(outputStr string) (model.Output, error) {
	// Handle the default case (regular file) if no :: is present
	if !strings.Contains(outputStr, "::") {
		return model.NewOutput(string(handlers.FileHandler), outputStr), nil
	}

	// Parse for output with explicit type
	parts := strings.SplitN(outputStr, "::", 2)
	if len(parts) != 2 {
		return model.Output{}, fmt.Errorf("invalid output format: %s", outputStr)
	}

	outputType := handlers.HandlerType(parts[0])
	identifier := parts[1]

	isKnownType := false
	for _, handlerType := range handlers.KnownHandlerTypes {
		if outputType == handlerType {
			isKnownType = true
			break
		}
	}

	if !isKnownType {
		return model.Output{}, fmt.Errorf("unknown output type: %s", outputType)
	}

	return model.NewOutput(string(outputType), identifier), nil
}
