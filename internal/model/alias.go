package model

import "grog/internal/label"

var _ BuildNode = &Alias{}

// Alias is a simple build node that points to another target.
type Alias struct {
	// The file in which this alias was defined
	SourceFilePath string `json:"-"`

	Label      label.TargetLabel `json:"label"`
	Actual     label.TargetLabel `json:"actual"`
	IsSelected bool              `json:"is_selected,omitempty"`
}

func (a *Alias) GetType() NodeType { return AliasNode }

func (a *Alias) GetLabel() label.TargetLabel { return a.Label }

func (a *Alias) GetDependencies() []label.TargetLabel { return []label.TargetLabel{a.Actual} }

func (a *Alias) Select() { a.IsSelected = true }

func (a *Alias) GetIsSelected() bool { return a.IsSelected }
