package loading

// TargetDTO is used for deserializing a target in a loader.
// The target is used internally is in model.Target.
type TargetDTO struct {
	Name    string   `json:"name" yaml:"name" pkl:"name"`
	Command string   `json:"cmd" yaml:"cmd" pkl:"cmd"`
	Deps    []string `json:"deps,omitempty" yaml:"deps,omitempty" pkl:"deps"`
	Inputs  []string `json:"inputs,omitempty" yaml:"inputs,omitempty" pkl:"inputs"`
	Outputs []string `json:"outputs,omitempty" yaml:"outputs,omitempty" pkl:"outputs"`
}

// PackageDTO is used for deserializing a package in a loader.
// The package that we use internally is in model.Package.
type PackageDTO struct {
	// Record the path to the source file that defines this package.
	// Used for logging
	SourceFilePath string

	Targets []*TargetDTO `json:"targets" yaml:"targets" pkl:"targets"`
}
