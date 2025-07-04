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

	Tags                 []string              `json:"tags,omitempty" yaml:"tags,omitempty" pkl:"tags"`
	Platform             *model.PlatformConfig `json:"platform,omitempty" yaml:"platform,omitempty" pkl:"platform"`
	EnvironmentVariables map[string]string     `json:"environment_variables,omitempty" yaml:"environment_variables,omitempty" pkl:"environment_variables"`
}

type AliasDTO struct {
	Name   string `json:"name" yaml:"name" pkl:"name"`
	Actual string `json:"actual" yaml:"actual" pkl:"actual"`
}

type PlatformConfigDTO struct {
	Os   []string `json:"os,omitempty" yaml:"os,omitempty" pkl:"os"`
	Arch []string `json:"arch,omitempty" yaml:"arch,omitempty" pkl:"arch"`
}

// PackageDTO is used for deserializing a package in a loader.
// The package that we use internally is in model.Package.
type PackageDTO struct {
	// Record the path to the source file that defines this package.
	// Used for logging
	SourceFilePath string

	Targets []*TargetDTO `json:"targets" yaml:"targets" pkl:"targets"`
	Aliases []*AliasDTO  `json:"aliases" yaml:"aliases" pkl:"aliases"`

	// DefaultPlatform specifies the platform selector at the package level.
	// This serves as the default for target-level platform selectors.
	// If a target specifies its own platform selector, it overrides this default.
	DefaultPlatform *model.PlatformConfig `json:"default_platform,omitempty" yaml:"default_platform,omitempty" pkl:"default_platform"`
}
