package shell

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCommandArguments(t *testing.T) {
	scriptPath := filepath.Join(t.TempDir(), "script with spaces.sh")
	if err := os.WriteFile(scriptPath, []byte(`printf '%s\n' "$@"`), 0600); err != nil {
		t.Fatal(err)
	}
	output, err := Command(t.Context(), scriptPath, "two words", "a&b", "").CombinedOutput()
	if err != nil || string(output) != "two words\na&b\n\n" {
		t.Fatalf("got %q, %v", output, err)
	}
}

func TestCommandCancellation(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
	defer cancel()
	started := time.Now()
	output, err := Command(ctx, "-c", "sleep 30 & wait").CombinedOutput()
	if err == nil || !errors.Is(ctx.Err(), context.DeadlineExceeded) || time.Since(started) > 5*time.Second {
		t.Fatalf("cancellation took %s: %v, output %s", time.Since(started), err, strings.TrimSpace(string(output)))
	}
}
