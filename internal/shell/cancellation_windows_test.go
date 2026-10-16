package shell

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCancellationStopsChildren(t *testing.T) {
	markerPath := filepath.Join(t.TempDir(), "leaked.txt")
	ctx, cancel := context.WithTimeout(t.Context(), 300*time.Millisecond)
	defer cancel()
	command := Command(ctx, "-c", `(sleep 2; touch "$1") & wait`, "grog", filepath.ToSlash(markerPath))
	if output, err := command.CombinedOutput(); err == nil {
		t.Fatalf("command should have timed out: %s", output)
	}
	time.Sleep(2500 * time.Millisecond)
	if _, err := os.Stat(markerPath); !os.IsNotExist(err) {
		t.Fatalf("child survived cancellation: %v", err)
	}
}
