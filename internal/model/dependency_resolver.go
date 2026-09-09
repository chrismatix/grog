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
}
