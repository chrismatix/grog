package execution

import (
	"bytes"
	"context"
	_ "embed"
	"errors"
	"fmt"
	"grog/internal/config"
	"grog/internal/model"
	"os/exec"
	"text/template"
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

func executeTarget(ctx context.Context, target *model.Target, binToolPaths BinToolMap) error {
	executionPath := config.GetPathAbsoluteToWorkspaceRoot(target.Label.Package)
	templatedCommand, err := getCommand(binToolPaths, target.Command)
	if err != nil {
		return err
	}

	cmd := exec.CommandContext(ctx, "sh", "-c", templatedCommand)

	cmd.Dir = executionPath

	cmdOut, err := cmd.CombinedOutput()

	if err != nil {
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
