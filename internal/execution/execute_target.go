package execution

import (
	"bytes"
	"context"
	_ "embed"
	"errors"
	"fmt"
	"grog/internal/config"
	"grog/internal/console"
	"grog/internal/model"
	"io"
	"os"
	"os/exec"
	"text/template"
	"time"
)

//go:embed run_sh.sh.tmpl
var binTemplate string

// templateData is the data expected by the run_sh.sh.tmpl
type templateData struct {
	BinToolMap  BinToolMap
	UserCommand string
}

// BinToolMap Maps target label to tool a binary path
type BinToolMap map[string]string

func executeTarget(
	ctx context.Context,
	target *model.Target,
	binToolPaths BinToolMap,
	streamLogs bool,
) error {
	cmdOut, err := runTargetCommand(ctx, target, binToolPaths, target.Command, streamLogs)

	if err != nil {
		if ctx.Err() != nil {
			// bubble up cancellation error
			return ctx.Err()
		}

		var exitError *exec.ExitError
		if errors.As(err, &exitError) {
			return &CommandError{
				TargetLabel: target.Label,
				ExitCode:    exitError.ExitCode(),
				Output:      string(cmdOut),
			}
		}
		return fmt.Errorf("target %s failed: %w - output: %s", target.Label, err, string(cmdOut))
	}
	return nil
}

// runTargetCommand runs a single shell command in the context of a target
func runTargetCommand(
	ctx context.Context,
	target *model.Target,
	binToolPaths BinToolMap,
	command string,
	streamLogs bool,
) ([]byte, error) {
	executionPath := config.GetPathAbsoluteToWorkspaceRoot(target.Label.Package)
	templatedCommand, err := getCommand(binToolPaths, command)
	if err != nil {
		return nil, err
	}

	cmd := exec.CommandContext(ctx, "sh", "-c", templatedCommand)
	cmd.WaitDelay = 1 * time.Second // cancellation grace time

	gitHash, err := config.GetGitHash()
	if err != nil {
		console.GetLogger(ctx).Debugf("failed to get git hash: %v", err)
	}

	// Attach env variables to the existing environment
	cmd.Env = append(os.Environ(),
		"GROG_TARGET="+target.Label.String(),
		"GROG_OS="+config.Global.OS,
		"GROG_ARCH="+config.Global.Arch,
		"GROG_PLATFORM="+config.Global.GetPlatform(),
		"GROG_PACKAGE="+target.Label.Package,
		"GROG_GIT_HASH="+gitHash,
	)
	cmd.Dir = executionPath

	var buffer bytes.Buffer

	if program := console.GetTeaProgram(ctx); program != nil && streamLogs {
		teaWriter := console.NewTeaWriter(program)
		multiOut := io.MultiWriter(&buffer, teaWriter)
		cmd.Stdout = multiOut
		cmd.Stderr = multiOut
	} else {
		cmd.Stdout = &buffer
		cmd.Stderr = &buffer
	}

	if cmdErr := cmd.Run(); cmdErr != nil {
		return buffer.Bytes(), cmdErr
	}
	return buffer.Bytes(), nil
}

func getCommand(toolMap BinToolMap, command string) (string, error) {
	tmpl, err := template.New("binCommand").Parse(binTemplate)
	if err != nil {
		return "", fmt.Errorf("failed to parse run template: %w", err)
	}

	data := templateData{
		BinToolMap:  toolMap,
		UserCommand: command,
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("failed to execute bin function template: %w", err)
	}

	return buf.String(), nil
}
