package model

import (
	"time"

	"grog/internal/label"
)

var _ BuildNode = &Resource{}

// Resource is a build node that manages the lifecycle of a shared external
// helper (e.g. a database container) instead of producing outputs. It is
// started at most once per invocation when the first dependent target
// executes and torn down when the build finishes. Resources are never cached
// and do not contribute to the change hash of their dependents.
type Resource struct {
	// The file in which this resource was defined
	SourceFilePath string `json:"-"`

	Label label.TargetLabel `json:"label"`

	// Up is the command that starts the resource. It must return once the
	// resource is started (daemonized), not block for its lifetime.
	Up string `json:"up"`
	// Down is the optional command that tears the resource down.
	Down string `json:"down,omitempty"`
	// Ready is an optional probe command that is polled until it exits 0
	// before dependents may run.
	Ready string `json:"ready,omitempty"`
	// Timeout bounds the start phase (up + ready polling) and, separately,
	// the down command. Zero means DefaultResourceTimeout.
	Timeout time.Duration `json:"timeout,omitempty"`

	// Exports are environment variables published to the commands of targets
	// that directly depend on this resource. The up command can add or
	// override entries by appending KEY=VALUE lines to the file at
	// $GROG_RESOURCE_EXPORTS_FILE.
	Exports map[string]string `json:"exports,omitempty"`

	Dependencies []label.TargetLabel `json:"dependencies,omitempty"`

	IsSelected bool `json:"is_selected,omitempty"`
}

// DefaultResourceTimeout is used when a resource does not declare a timeout.
const DefaultResourceTimeout = 5 * time.Minute

func (r *Resource) GetType() NodeType { return ResourceNode }

func (r *Resource) GetLabel() label.TargetLabel { return r.Label }

func (r *Resource) GetDependencies() []label.TargetLabel { return r.Dependencies }

func (r *Resource) Select() { r.IsSelected = true }

func (r *Resource) GetIsSelected() bool { return r.IsSelected }

func (r *Resource) GetTimeout() time.Duration {
	if r.Timeout > 0 {
		return r.Timeout
	}
	return DefaultResourceTimeout
}
