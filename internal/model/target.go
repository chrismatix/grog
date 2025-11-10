package model

import (
	"encoding/json"
	"grog/internal/label"
	"strings"
	"time"
)

var _ BuildNode = &Target{}

// Target defines a build step that depends on Dependencies (other targets)
// and Inputs (files) and produces Outputs.
type Target struct {
	Label label.TargetLabel `json:"label"`
	// The file in which this target was defined
	SourceFilePath string `json:"-"`

	Command              string              `json:"cmd"`
	Dependencies         []label.TargetLabel `json:"dependencies,omitempty"`
	Inputs               []string            `json:"inputs,omitempty"`
	ExcludeInputs        []string            `json:"exclude_inputs,omitempty"`
	Outputs              []Output            `json:"outputs,omitempty"`
	Platforms            []string            `json:"platforms,omitempty" yaml:"platforms,omitempty" pkl:"platforms"`
	Tags                 []string            `json:"tags,omitempty"`
	Fingerprint          map[string]string   `json:"fingerprint,omitempty"`
	EnvironmentVariables map[string]string   `json:"environment_variables,omitempty"`
	OutputChecks         []OutputCheck       `json:"output_checks,omitempty"`
	Timeout              time.Duration       `json:"timeout,omitempty"`

	// UnresolvedInputs are the inputs as specified by the user (no glob resolving)
	UnresolvedInputs []string `json:"-"`
	// BinOutput is always a path to a binary file
	BinOutput Output `json:"bin_output,omitempty"`
	// Whether this target is selected for execution.
	IsSelected bool `json:"is_selected,omitempty"`
	// Whether the outputs for this target were already loaded in the current execution
	OutputsLoaded bool `json:"outputs_loaded,omitempty"`

	// ChangeHash is the combined hash of the target definition and its input files
	ChangeHash  string `json:"change_hash,omitempty"`
	HasCacheHit bool   `json:"has_cache_hit,omitempty"`
}

type OutputCheck struct {
	Command        string `json:"command" yaml:"command" pkl:"command"`
	ExpectedOutput string `json:"expected_output,omitempty" yaml:"expected_output,omitempty" pkl:"expected_output"`
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

func (t *Target) SkipsCache() bool {
	for _, tag := range t.Tags {
		if tag == "no-cache" {
			return true
		}
	}
	return false
}

func (t *Target) IsMultiplatformCache() bool {
	for _, tag := range t.Tags {
		if tag == "multiplatform-cache" {
			return true
		}
	}
	return false
}

func (t *Target) CommandEllipsis() string {
	lines := strings.SplitN(t.Command, "\n", 2)
	firstLine := strings.TrimLeft(lines[0], " ")
	if len(firstLine) > 70 {
		return firstLine[:67] + "..."
	} else if len(lines) > 1 {
		return firstLine + "..."
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

// HasOutputChecksOnly checks if this is a special type of target that only had output checks
// and no in or outputs. In that case the output checks are the only thing that should determine
// if a target needs to be run (
func (t *Target) HasOutputChecksOnly() bool {
	return len(t.OutputChecks) > 0 && len(t.AllOutputs()) == 0 && len(t.Inputs) == 0
}

func PrintSortedLabels(nodes []BuildNode) {
	labels := make([]label.TargetLabel, len(nodes))
	for i, target := range nodes {
		labels[i] = target.GetLabel()
	}
	label.PrintSorted(labels)
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

// -------------------------------
// BuildNode interface implementation

func (t *Target) GetType() NodeType { return TargetNode }

func (t *Target) GetLabel() label.TargetLabel {
	return t.Label
}

func (t *Target) GetDependencies() []label.TargetLabel {
	return t.Dependencies
}

func (t *Target) Select() {
	t.IsSelected = true
}

func (t *Target) GetIsSelected() bool {
	return t.IsSelected
}
