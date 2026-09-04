package loading

import (
	"fmt"
	"time"
)

// ValidatePackageTimeouts validates duration fields accepted by package loaders.
func ValidatePackageTimeouts(packageDTO PackageDTO) error {
	for _, target := range packageDTO.Targets {
		if _, operationError := parseBuildTimeout(targetDeclarationKind, target.Name, target.Timeout); operationError != nil {
			return operationError
		}
	}
	for _, resource := range packageDTO.Resources {
		if _, operationError := parseBuildTimeout(resourceDeclarationKind, resource.Name, resource.Timeout); operationError != nil {
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
