package loading

import (
	"encoding/json"
	"fmt"

	"grog/internal/model"

	"gopkg.in/yaml.v3"
)

// ociPushDestinations holds the value side of TargetDTO.OciPush: one or
// more remote tags. Authors can write a scalar for the common single-
// destination case and a list for multi-destination; both deserialise here.
type ociPushDestinations []string

func (o *ociPushDestinations) UnmarshalYAML(node *yaml.Node) error {
	switch node.Kind {
	case yaml.ScalarNode:
		*o = []string{node.Value}
		return nil
	case yaml.SequenceNode:
		dst := make([]string, 0, len(node.Content))
		for _, item := range node.Content {
			if item.Kind != yaml.ScalarNode {
				return fmt.Errorf("oci_push destination must be a string, got %v", item.Kind)
			}
			dst = append(dst, item.Value)
		}
		*o = dst
		return nil
	default:
		return fmt.Errorf("oci_push destination must be a string or list of strings")
	}
}

func (o *ociPushDestinations) UnmarshalJSON(data []byte) error {
	var single string
	if err := json.Unmarshal(data, &single); err == nil {
		*o = []string{single}
		return nil
	}
	var list []string
	if err := json.Unmarshal(data, &list); err != nil {
		return fmt.Errorf("oci_push destination must be a string or list of strings: %w", err)
	}
	*o = list
	return nil
}

// TargetDTO is used for deserializing a target in a loader.
// The target is used internally is in model.Target.
type TargetDTO struct {
	Name          string   `json:"name" yaml:"name" pkl:"name" starlark:"name"`
	Command       string   `json:"command" yaml:"command" pkl:"command" starlark:"command"`
	Dependencies  []string `json:"dependencies,omitempty" yaml:"dependencies,omitempty" pkl:"dependencies" starlark:"dependencies"`
	Inputs        []string `json:"inputs,omitempty" yaml:"inputs,omitempty" pkl:"inputs" starlark:"inputs"`
	ExcludeInputs []string `json:"exclude_inputs,omitempty" yaml:"exclude_inputs,omitempty" pkl:"exclude_inputs" starlark:"exclude_inputs"`
	Outputs       []string `json:"outputs,omitempty" yaml:"outputs,omitempty" pkl:"outputs" starlark:"outputs"`
	// OciPush is a map from an oci:: output's local name to its remote
	// destination(s). A scalar value is normalised to a single-entry slice
	// at parse time so YAML and pkl can both write `name: "repo:tag"`.
	OciPush            map[string]ociPushDestinations `json:"oci_push,omitempty" yaml:"oci_push,omitempty" pkl:"oci_push" starlark:"oci_push"`
	BinOutput          string                         `json:"bin_output" yaml:"bin_output" pkl:"bin_output" starlark:"bin_output"`
	BinaryRequiresPush bool                           `json:"binary_requires_push,omitempty" yaml:"binary_requires_push,omitempty" pkl:"binary_requires_push" starlark:"binary_requires_push"`

	OutputChecks []model.OutputCheck `json:"output_checks,omitempty" yaml:"output_checks,omitempty" pkl:"output_checks" starlark:"output_checks"`

	Tags                 []string          `json:"tags,omitempty" yaml:"tags,omitempty" pkl:"tags" starlark:"tags"`
	Fingerprint          map[string]string `json:"fingerprint,omitempty" yaml:"fingerprint,omitempty" pkl:"fingerprint" starlark:"fingerprint"`
	Platforms            []string          `json:"platforms,omitempty" yaml:"platforms,omitempty" pkl:"platforms" starlark:"platforms"`
	EnvironmentVariables map[string]string `json:"environment_variables,omitempty" yaml:"environment_variables,omitempty" pkl:"environment_variables" starlark:"environment_variables"`
	Timeout              string            `json:"timeout,omitempty" yaml:"timeout,omitempty" pkl:"timeout" starlark:"timeout"`

	ConcurrencyGroup string `json:"concurrency_group,omitempty" yaml:"concurrency_group,omitempty" pkl:"concurrency_group" starlark:"concurrency_group"`
}

type AliasDTO struct {
	Name   string `json:"name" yaml:"name" pkl:"name" starlark:"name"`
	Actual string `json:"actual" yaml:"actual" pkl:"actual" starlark:"actual"`
}

type EnvironmentDTO struct {
	Name         string   `json:"name" yaml:"name" pkl:"name" starlark:"name"`
	Type         string   `json:"type" yaml:"type" pkl:"type" starlark:"type"`
	Dependencies []string `json:"dependencies,omitempty" yaml:"dependencies,omitempty" pkl:"dependencies" starlark:"dependencies"`
	OCIImage     string   `json:"oci_image" yaml:"oci_image" pkl:"oci_image" starlark:"oci_image"`
}

// PackageDTO is used for deserializing a package in a loader.
// The package that we use internally is in model.Package.
type PackageDTO struct {
	// Record the path to the source file that defines this package.
	// Note that in the final model package this is stored on the target level not the package
	SourceFilePath string

	Targets      []*TargetDTO      `json:"targets" yaml:"targets" pkl:"targets" starlark:"targets"`
	Aliases      []*AliasDTO       `json:"aliases" yaml:"aliases" pkl:"aliases" starlark:"aliases"`
	Environments []*EnvironmentDTO `json:"environments" yaml:"environments" pkl:"environments" starlark:"environments"`

	// DefaultPlatforms specifies the platform selectors at the package level.
	// This serves as the default for target-level platform selectors.
	// If a target specifies its own platform selectors, they override this default.
	DefaultPlatforms []string `json:"default_platforms,omitempty" yaml:"default_platforms,omitempty" pkl:"default_platforms" starlark:"default_platforms"`
}
