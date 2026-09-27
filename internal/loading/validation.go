package loading

import (
	"fmt"
	"time"

	"grog/internal/label"
	"grog/internal/model"
	"grog/internal/output"
)

// ValidatePackageTimeouts validates duration fields accepted by package loaders.
func ValidatePackageTimeouts(packageDTO PackageDTO) error {
	for _, target := range packageDTO.Targets {
		if target == nil {
			continue
		}
		if _, operationError := parseBuildTimeout(targetDeclarationKind, target.Name, target.Timeout); operationError != nil {
			return operationError
		}
	}
	for _, resource := range packageDTO.Resources {
		if resource == nil {
			continue
		}
		if _, operationError := parseBuildTimeout(resourceDeclarationKind, resource.Name, resource.Timeout); operationError != nil {
			return operationError
		}
	}
	return nil
}

// ValidatePackageLabels validates every target label accepted by package enrichment.
func ValidatePackageLabels(packageDTO PackageDTO, packagePath string) error {
	for _, target := range packageDTO.Targets {
		if target == nil {
			continue
		}
		if operationError := validateLabels(packagePath, target.Dependencies); operationError != nil {
			return operationError
		}
	}
	for _, resource := range packageDTO.Resources {
		if resource == nil {
			continue
		}
		if operationError := validateLabels(packagePath, resource.Dependencies); operationError != nil {
			return operationError
		}
	}
	for _, alias := range packageDTO.Aliases {
		if alias == nil {
			continue
		}
		if _, operationError := label.ParseTargetLabel(packagePath, alias.Actual); operationError != nil {
			return operationError
		}
	}
	return nil
}

// ValidatePackageOutputs validates output fields accepted by package enrichment.
func ValidatePackageOutputs(packageDTO PackageDTO) error {
	for _, target := range packageDTO.Targets {
		if target == nil {
			continue
		}
		if _, _, operationError := parseTargetOutputs(target, target.Name); operationError != nil {
			return operationError
		}
	}
	return nil
}

func parseTargetOutputs(target *TargetDTO, targetName string) ([]model.Output, model.Output, error) {
	parsedOutputs, operationError := output.ParseOutputs(target.Outputs)
	if operationError != nil {
		return nil, model.Output{}, fmt.Errorf("failed to parse outputs for target %s: %w", targetName, operationError)
	}
	binaryOutput := model.Output{}
	if target.BinOutput == "" {
		return parsedOutputs, binaryOutput, nil
	}
	binaryOutput, operationError = output.ParseOutput(target.BinOutput)
	if operationError != nil {
		return nil, model.Output{}, fmt.Errorf("failed to parse bin output for target %s: %w", targetName, operationError)
	}
	if !binaryOutput.IsFile() {
		return nil, model.Output{}, fmt.Errorf("bin output %s for target %s must be of type file", target.BinOutput, targetName)
	}
	return parsedOutputs, binaryOutput, nil
}

func validateLabels(packagePath string, values []string) error {
	for _, value := range values {
		if _, operationError := label.ParseTargetLabel(packagePath, value); operationError != nil {
			return operationError
		}
	}
	return nil
}

func parseBuildTimeout(declarationKind string, declarationName string, value string) (time.Duration, error) {
	if value == "" {
		return 0, nil
	}
	duration, operationError := time.ParseDuration(value)
	if operationError != nil {
		return 0, fmt.Errorf("failed to parse timeout for %s %s: %w", declarationKind, declarationName, operationError)
	}
	return duration, nil
}
