package loading

// TargetDTO is used for deserializing a target in a loader.
// The target that we use internally is in model.Target.
type TargetDTO struct {
	Command string   `json:"cmd"`
	Deps    []string `json:"deps,omitempty"`
	Inputs  []string `json:"inputs,omitempty"`
	Outputs []string `json:"outputs,omitempty"`
}

// PackageDTO is used for deserializing a package in a loader.
// The package that we use internally is in model.Package.
type PackageDTO struct {
	// Record the path to the source file that defines this package.
	// for logging purposes
	SourceFilePath string

	Targets map[string]*TargetDTO `json:"targets"`
}
