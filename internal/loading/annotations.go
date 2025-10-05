package loading

// grogAnnotation represents the annotation block in a Makefile.
type grogAnnotation struct {
	Name         string   `yaml:"name"`
	Dependencies []string `yaml:"dependencies"`
	Inputs       []string `yaml:"inputs"`
	Outputs      []string `yaml:"outputs"`
	Tags         []string `yaml:"tags"`
}
