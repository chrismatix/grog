package model

// Package defines all the information that a package needs to build.
type Package struct {
	// Record the path to the source file that defines this package.
	// for logging purposes
	SourceFilePath string

	Targets map[string]*Target `json:"targets"`
}
