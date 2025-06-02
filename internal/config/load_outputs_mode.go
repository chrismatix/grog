package config

import "fmt"

// LoadOutputsMode determines what outputs to load
type LoadOutputsMode int

const (
	// LoadOutputsAll loads all outputs
	LoadOutputsAll LoadOutputsMode = iota
	// LoadOutputsMinimal only loads an output if a direct dependant needs to be re-built
	LoadOutputsMinimal
)

func (m LoadOutputsMode) String() string {
	switch m {
	case LoadOutputsAll:
		return "all"
	default:
		return "minimal"
	}
}

// ParseLoadOutputsMode converts a string to a LoadOutputsMode
// Returns an error if the string is not a valid mode
func ParseLoadOutputsMode(s string) (LoadOutputsMode, error) {
	switch s {
	case "all":
		return LoadOutputsAll, nil
	case "minimal":
		return LoadOutputsMinimal, nil
	default:
		return LoadOutputsAll, fmt.Errorf("invalid load_outputs mode: '%s'. Must be either 'all' or 'minimal'", s)
	}
}
