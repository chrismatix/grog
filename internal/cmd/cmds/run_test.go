package cmds

import (
	"os/exec"
	"strings"
	"testing"
)

func TestCaptureBinaryOutputPreservesStdin(t *testing.T) {
	command := exec.Command("ignored")
	stdin := strings.NewReader("input")
	command.Stdin = stdin

	output := captureBinaryOutput(command)
	if command.Stdin != stdin {
		t.Error("expected stdin to remain attached")
	}
	if command.Stdout != output || command.Stderr != output {
		t.Error("expected stdout and stderr to use the capture buffer")
	}
}
