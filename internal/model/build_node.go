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
	// Deselect clears the selection flag. Used by embedders (e.g. the session
	// API) that reuse a single loaded graph across multiple sequential builds
	// and must reset selection between them.
	Deselect()
	GetIsSelected() bool
	GetType() NodeType
}

func IsTestTargetNode(node BuildNode) bool {
	if target, ok := node.(*Target); ok {
		return target.IsTest()
	}
	return false
}
