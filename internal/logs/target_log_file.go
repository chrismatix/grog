package logs

import (
	"fmt"
	"grog/internal/config"
	"grog/internal/model"
	"io"
	"os"
	"path/filepath"
)

type TargetLogFile struct {
	workspaceDirectory string
	target             model.Target
}

func NewTargetLogFile(target model.Target) *TargetLogFile {
	return &TargetLogFile{
		workspaceDirectory: filepath.Join(config.Global.GetWorkspaceRootDir(), "logs"),
		target:             target,
	}
}

// Path returns the path of the latest/current log file for a given target
// -> {targetPackagePath}/{targetName}.txt
func (tl *TargetLogFile) Path() string {
	targetPath := fmt.Sprintf(
		"%s/%s.txt",
		tl.target.Label.Package,
		tl.target.Label.Name)

	return filepath.Join(tl.workspaceDirectory, targetPath)
}

func (tl *TargetLogFile) Open() (*os.File, error) {
	logFilePath := tl.Path()
	logFileDir := filepath.Dir(logFilePath)

	// Ensure the directory exists
	if err := os.MkdirAll(logFileDir, 0755); err != nil {
		return nil, err
	}

	return os.OpenFile(tl.Path(), os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
}

func (tl *TargetLogFile) Exists() bool {
	_, err := os.Stat(tl.Path())
	return err == nil
}

// Print prints out the entire file
func (tl *TargetLogFile) Print() error {
	file, err := os.Open(tl.Path())
	if err != nil {
		return err
	}

	defer file.Close()
	_, err = io.Copy(os.Stdout, file)
	return err
}
