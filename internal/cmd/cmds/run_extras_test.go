package cmds

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"grog/internal/config"
)

func TestRunScriptFile_PathOutsideWorkspace(t *testing.T) {
	prev := config.Global
	tmp := t.TempDir()
	config.Global = config.WorkspaceConfig{WorkspaceRoot: tmp, Root: tmp}
	t.Cleanup(func() { config.Global = prev })

	outside := t.TempDir()
	scriptPath := filepath.Join(outside, "script.sh")
	if err := os.WriteFile(scriptPath, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	logger := newTestLogger()
	err := runScriptFile(context.Background(), logger, scriptPath, nil)
	if err == nil {
		t.Fatal("expected err for path outside workspace")
	}
}

func TestRunScriptFile_NotFound(t *testing.T) {
	prev := config.Global
	tmp := t.TempDir()
	config.Global = config.WorkspaceConfig{WorkspaceRoot: tmp, Root: tmp}
	t.Cleanup(func() { config.Global = prev })

	logger := newTestLogger()
	err := runScriptFile(context.Background(), logger, filepath.Join(tmp, "missing.sh"), nil)
	if err == nil {
		t.Fatal("expected err")
	}
}
