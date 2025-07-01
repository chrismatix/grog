package model

import "grog/internal/label"

// Environment defines a build environment configuration which can be referenced
// by targets or other environments.
type Environment struct {
	Label        label.TargetLabel    `json:"label" yaml:"label"`
	Dependencies []label.TargetLabel  `json:"dependencies,omitempty" yaml:"dependencies,omitempty"`
	Command      string               `json:"command,omitempty" yaml:"command,omitempty"`
	OutputImage  string               `json:"output_image,omitempty" yaml:"output_image,omitempty"`
	Inputs       []string             `json:"inputs,omitempty" yaml:"inputs,omitempty"`
	Defaults     *EnvironmentDefaults `json:"defaults,omitempty" yaml:"defaults,omitempty"`
}

// EnvironmentDefaults capture default settings for environment usage.
type EnvironmentDefaults struct {
	MountDependencies     string `json:"mount_dependencies,omitempty" yaml:"mount_dependencies,omitempty"`
	MountDependencyInputs bool   `json:"mount_dependency_inputs,omitempty" yaml:"mount_dependency_inputs,omitempty"`
}

func (e *Environment) GetLabel() label.TargetLabel          { return e.Label }
func (e *Environment) GetDependencies() []label.TargetLabel { return e.Dependencies }
