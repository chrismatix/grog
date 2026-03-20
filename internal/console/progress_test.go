package console

import (
	"strings"
	"testing"
)

func TestFormatProgressBarIncludesBytes(t *testing.T) {
	progress := Progress{Current: 512 * 1024, Total: 1024 * 1024}

	formatted := formatProgressBar(progress, 10)
	if formatted == "" {
		t.Fatalf("expected formatted progress string")
	}

	if want := "512.0 KB/1.0 MB"; !strings.Contains(formatted, want) {
		t.Fatalf("expected progress to include byte counts %q, got %q", want, formatted)
	}
}

func TestFormatBytesSmallValues(t *testing.T) {
	if got := formatBytes(512); got != "512 B" {
		t.Fatalf("expected unscaled bytes, got %q", got)
	}

	if got := formatBytes(2048); got != "2.0 KB" {
		t.Fatalf("expected kilobytes, got %q", got)
	}
}
