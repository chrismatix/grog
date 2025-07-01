package model

import "grog/internal/label"

// BuildNode represents a node in the build graph. It is implemented by
// regular build targets and environment targets.
type BuildNode interface {
	GetLabel() label.TargetLabel
	GetDependencies() []label.TargetLabel
}
