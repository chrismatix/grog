package loading

// scriptAnnotation represents a grog yaml annotation for the grog scripts feature
type scriptAnnotation struct {
	Name                 string            `yaml:"name"`
	Dependencies         []string          `yaml:"dependencies"`
	Inputs               []string          `yaml:"inputs"`
	Tags                 []string          `yaml:"tags"`
	Fingerprint          map[string]string `yaml:"fingerprint"`
	EnvironmentVariables map[string]string `yaml:"environment_variables"`
	Timeout              string            `yaml:"timeout"`
	Platforms            []string          `yaml:"platforms"`
}

// grogAnnotation represents the annotation block in a Makefile.
type grogAnnotation struct {
	scriptAnnotation `yaml:",inline"`
	// script annotations cannot have outputs
	Outputs []string `yaml:"outputs"`
}
