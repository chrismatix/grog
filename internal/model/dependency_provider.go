package model

import (
	"grog/internal/label"
	"time"
)

// DependencyProvider declares a command that infers dependencies during loading.
type DependencyProvider struct {
	SourceFilePath string
	Label          label.TargetLabel
	Command        string
	Inputs         []string
	Timeout        time.Duration
}
