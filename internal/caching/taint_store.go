package caching

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	"grog/internal/config"
	"grog/internal/label"
)

// TaintStore tracks targets that the user has marked for forced rebuild.
// Taint is per-checkout control state — it never writes to remote caches
// and never crosses workspace boundaries, even when multiple workspaces
// share a GROG_ROOT. The on-disk layout mirrors the label under
// $GROG_ROOT/<workspace-prefix>/taint/.
type TaintStore struct {
	dir string
}

// NewTaintStore returns a TaintStore rooted at the current workspace's
// per-checkout taint directory.
func NewTaintStore() *TaintStore {
	return &TaintStore{
		dir: filepath.Join(config.Global.GetWorkspaceRootDir(), "taint"),
	}
}

func (ts *TaintStore) entryPath(targetLabel label.TargetLabel) string {
	return filepath.Join(ts.dir, targetLabel.String())
}

// Taint marks a target as pending rebuild. Idempotent.
func (ts *TaintStore) Taint(_ context.Context, targetLabel label.TargetLabel) error {
	entryPath := ts.entryPath(targetLabel)
	if err := os.MkdirAll(filepath.Dir(entryPath), 0755); err != nil {
		return err
	}
	f, err := os.OpenFile(entryPath, os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	return f.Close()
}

// IsTainted reports whether a target has been marked for rebuild.
func (ts *TaintStore) IsTainted(_ context.Context, targetLabel label.TargetLabel) (bool, error) {
	_, err := os.Stat(ts.entryPath(targetLabel))
	if err == nil {
		return true, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return false, err
}

// Clear removes a target's taint mark. Clearing an untainted target is a
// no-op.
func (ts *TaintStore) Clear(_ context.Context, targetLabel label.TargetLabel) error {
	if err := os.Remove(ts.entryPath(targetLabel)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}
