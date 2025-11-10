package loading

import "grog/internal/model"

// TargetDTO is used for deserializing a target in a loader.
// The target is used internally is in model.Target.
type TargetDTO struct {
	Name          string   `json:"name" yaml:"name" pkl:"name"`
	Command       string   `json:"command" yaml:"command" pkl:"command"`
	Dependencies  []string `json:"dependencies,omitempty" yaml:"dependencies,omitempty" pkl:"dependencies"`
	Inputs        []string `json:"inputs,omitempty" yaml:"inputs,omitempty" pkl:"inputs"`
	ExcludeInputs []string `json:"exclude_inputs,omitempty" yaml:"exclude_inputs,omitempty" pkl:"exclude_inputs"`
	Outputs       []string `json:"outputs,omitempty" yaml:"outputs,omitempty" pkl:"outputs"`
	BinOutput     string   `json:"bin_output" yaml:"bin_output" pkl:"bin_output"`

	OutputChecks []model.OutputCheck `json:"output_checks,omitempty" yaml:"output_checks,omitempty" pkl:"output_checks"`

	Tags                 []string          `json:"tags,omitempty" yaml:"tags,omitempty" pkl:"tags"`
	Fingerprint          map[string]string `json:"fingerprint,omitempty" yaml:"fingerprint,omitempty" pkl:"fingerprint"`
	Platforms            []string          `json:"platforms,omitempty" yaml:"platforms,omitempty" pkl:"platforms"`
	EnvironmentVariables map[string]string `json:"environment_variables,omitempty" yaml:"environment_variables,omitempty" pkl:"environment_variables"`
	Timeout              string            `json:"timeout,omitempty" yaml:"timeout,omitempty" pkl:"timeout"`
}

type AliasDTO struct {
	Name   string `json:"name" yaml:"name" pkl:"name"`
	Actual string `json:"actual" yaml:"actual" pkl:"actual"`
}

type EnvironmentDTO struct {
	Name         string   `json:"name" yaml:"name" pkl:"name"`
	Type         string   `json:"type" yaml:"type" pkl:"type"`
	Dependencies []string `json:"dependencies,omitempty" yaml:"dependencies,omitempty" pkl:"dependencies"`
	DockerImage  string   `json:"docker_image" yaml:"docker_image" pkl:"docker_image"`
}

// PackageDTO is used for deserializing a package in a loader.
// The package that we use internally is in model.Package.
type PackageDTO struct {
	// Record the path to the source file that defines this package.
	// Note that in the final model package this is stored on the target level not the package
	SourceFilePath string

	Targets      []*TargetDTO      `json:"targets" yaml:"targets" pkl:"targets"`
	Aliases      []*AliasDTO       `json:"aliases" yaml:"aliases" pkl:"aliases"`
	Environments []*EnvironmentDTO `json:"environments" yaml:"environments" pkl:"environments"`

	// DefaultPlatforms specifies the platform selectors at the package level.
	// This serves as the default for target-level platform selectors.
	// If a target specifies its own platform selectors, they override this default.
	DefaultPlatforms []string `json:"default_platforms,omitempty" yaml:"default_platforms,omitempty" pkl:"default_platforms"`
}
