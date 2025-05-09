package model

import (
	"encoding/json"
	"grog/internal/label"
	"strings"
)

// Target defines a build step that depends on Dependencies (other targets)
// and Inputs (files) and produces Outputs.
type Target struct {
	Label label.TargetLabel `json:"label"`

	Command      string              `json:"cmd"`
	Dependencies []label.TargetLabel `json:"dependencies,omitempty"`
	Inputs       []string            `json:"inputs,omitempty"`
	Outputs      []Output            `json:"outputs,omitempty"`
	Platform     *PlatformConfig     `json:"platform,omitempty"`
	Tags         []string            `json:"tags,omitempty"`

	// BinOutput is always a path to a binary file
	BinOutput Output `json:"bin_output,omitempty"`
	// Whether this target is selected for execution.
	IsSelected bool `json:"is_selected,omitempty"`

	// ChangeHash is the combined hash of the target definition and its input files
	ChangeHash  string `json:"change_hash,omitempty"`
	HasCacheHit bool   `json:"has_cache_hit,omitempty"`
}

type PlatformConfig struct {
	OS   []string `json:"os,omitempty" yaml:"os,omitempty" pkl:"os"`
	Arch []string `json:"arch,omitempty" yaml:"arch,omitempty" pkl:"arch"`
}

func (t *Target) AllOutputs() []Output {
	if t.HasBinOutput() {
		return append(t.Outputs, t.BinOutput)
	}
	return t.Outputs
}

func (t *Target) HasBinOutput() bool {
	return t.BinOutput.IsSet()
}

func (t *Target) GetDepsString() []string {
	stringDeps := make([]string, len(t.Dependencies))
	for i, dep := range t.Dependencies {
		stringDeps[i] = dep.String()
	}
	return stringDeps
}

func (t *Target) CommandEllipsis() string {
	firstLine := strings.SplitN(t.Command, "\n", 2)[0]
	if len(firstLine) > 70 {
		return firstLine[:67] + "..."
	}
	return firstLine
}

func (t *Target) IsTest() bool {
	return t.Label.IsTest()
}

func (t *Target) FileOutputs() []string {
	var filePaths []string
	for _, output := range t.AllOutputs() {
		if output.IsFile() {
			// Identifier will be the path of the file
			filePaths = append(filePaths, output.Identifier)
		}
	}
	return filePaths
}

func (t *Target) OutputDefinitions() []string {
	var definitions []string
	for _, output := range t.AllOutputs() {
		// String() will return the full output definition as specified by the user
		definitions = append(definitions, output.String())
	}
	return definitions
}

// Alias to avoid infinite recursion in MarshalJSON
type targetAlias Target

func (t *Target) MarshalJSON() ([]byte, error) {
	// embed all fields via alias, override BinOutput to a pointer so omitempty works
	wrapper := struct {
		*targetAlias
		BinOutput *Output `json:"bin_output,omitempty"`
	}{
		targetAlias: (*targetAlias)(t),
	}

	if t.HasBinOutput() {
		// only include when set
		wrapper.BinOutput = &t.BinOutput
	}

	return json.Marshal(wrapper)
}
