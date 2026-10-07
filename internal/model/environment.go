package model

import (
	"time"

	"grog/internal/label"
)

var _ BuildNode = &Environment{}

// Environment is a build node that runs the commands of the targets that
// reference it through a provider: an executable that grog calls with
// GROG_ENV_PHASE set to up (once per invocation, lazily), exec (once per
// target run) and down (once when the build finishes).
type Environment struct {
	// The file in which this environment was defined
	SourceFilePath string `json:"-"`

	Label label.TargetLabel `json:"label"`

	// Provider is the shell command implementing the provider protocol, or the
	// name of a built-in provider such as builtin::docker.
	Provider string `json:"provider"`
	// Config is handed to the provider as JSON and is opaque to grog.
	Config map[string]string `json:"config,omitempty"`
	// Inputs are the files the provider depends on, e.g. its script.
	Inputs      []string          `json:"inputs,omitempty"`
	Fingerprint map[string]string `json:"fingerprint,omitempty"`
	// Timeout bounds the up and, separately, the down phase. Zero means
	// DefaultEnvironmentTimeout.
	Timeout time.Duration `json:"timeout,omitempty"`

	Dependencies []label.TargetLabel `json:"dependencies,omitempty"`

	IsSelected bool `json:"is_selected,omitempty"`

	// IdentityHash covers everything that decides how the environment runs a
	// command. It feeds the change hash of every target in the environment.
	IdentityHash string `json:"identity_hash,omitempty"`
}

// DockerProvider is the built-in provider that runs commands with docker run.
const DockerProvider = "builtin::docker"

// DefaultEnvironmentTimeout is used when an environment does not declare a timeout.
const DefaultEnvironmentTimeout = 5 * time.Minute

func (e *Environment) GetType() NodeType { return EnvironmentNode }

func (e *Environment) GetLabel() label.TargetLabel { return e.Label }

func (e *Environment) GetDependencies() []label.TargetLabel { return e.Dependencies }

func (e *Environment) Select() { e.IsSelected = true }

func (e *Environment) GetIsSelected() bool { return e.IsSelected }

func (e *Environment) GetTimeout() time.Duration {
	if e.Timeout > 0 {
		return e.Timeout
	}
	return DefaultEnvironmentTimeout
}
