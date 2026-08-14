package loading

// scriptAnnotation represents a grog yaml annotation for the grog scripts feature.
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

// annotationLineRange returns the first and last source line of an annotation
// block, or (0, 0) when the block carries no comment lines at all.
func annotationLineRange(annotationLineNumbers []int) (int, int) {
	if len(annotationLineNumbers) == 0 {
		return 0, 0
	}
	return annotationLineNumbers[0], annotationLineNumbers[len(annotationLineNumbers)-1]
}

// grogAnnotation represents the annotation block in a Makefile.
type grogAnnotation struct {
	scriptAnnotation `yaml:",inline"`

	// script annotations cannot have outputs
	Outputs []string                       `yaml:"outputs"`
	OciPush map[string]ociPushDestinations `yaml:"oci_push"`
}
