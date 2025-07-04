package model

import "grog/internal/label"

// NodeType represents the type of a build node
type NodeType string

const (
	TargetNode NodeType = "target"
	AliasNode  NodeType = "alias"
)

// BuildNode represents a node in the build graph. It is implemented by
// regular build targets and environment targets.
type BuildNode interface {
	GetLabel() label.TargetLabel
	GetDependencies() []label.TargetLabel
	Select()
	GetIsSelected() bool
	GetType() NodeType
}
