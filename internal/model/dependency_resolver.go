package model

import (
	"grog/internal/label"
	"time"
)

// DependencyResolver declares a command that infers dependencies during loading.
type DependencyResolver struct {
	SourceFilePath string
	Label          label.TargetLabel
	Command        string
	Inputs         []string
	Timeout        time.Duration
	// SynthesizedTarget is the name of the filegroup created for a package
	// the resolver reports but no target registers for.
	SynthesizedTarget string
}
