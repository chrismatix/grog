package model

import "grog/internal/label"

var _ BuildNode = &Environment{}

// Environment defines a Docker execution environment that targets can run inside.
// Environments are nodes in the build graph — their dependencies (e.g. image-building
// targets) are built before any target that references the environment.
type Environment struct {
	// The file in which this environment was defined
	SourceFilePath string `json:"-"`

	Label        label.TargetLabel   `json:"label"`
	Type         string              `json:"type"` // "docker"
	Dependencies []label.TargetLabel `json:"dependencies,omitempty"`
	DockerImage  string              `json:"docker_image"`
	IsSelected   bool                `json:"is_selected,omitempty"`
}

// BuildNode interface implementation

func (e *Environment) GetType() NodeType { return EnvironmentNode }

func (e *Environment) GetLabel() label.TargetLabel { return e.Label }

func (e *Environment) GetDependencies() []label.TargetLabel { return e.Dependencies }

func (e *Environment) Select() { e.IsSelected = true }

func (e *Environment) GetIsSelected() bool { return e.IsSelected }
