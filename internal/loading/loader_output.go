package loading

// TargetDto is used for deserializing a target in a loader.
// The target that we use internally is in model.Target.
type TargetDto struct {
	Command string   `json:"cmd" yaml:"cmd" pkl:"cmd"`
	Deps    []string `json:"deps,omitempty" yaml:"deps,omitempty" pkl:"deps,omitempty"`
	Inputs  []string `json:"inputs,omitempty" yaml:"inputs,omitempty" pkl:"inputs,omitempty"`
	Outputs []string `json:"outputs,omitempty" yaml:"outputs,omitempty" pkl:"outputs,omitempty"`
}

// PackageDto is used for deserializing a package in a loader.
// The package that we use internally is in model.Package.
type PackageDto struct {
	// Record the path to the source file that defines this package.
	// Used for logging
	SourceFilePath string

	Targets map[string]*TargetDto `json:"targets"`
}
