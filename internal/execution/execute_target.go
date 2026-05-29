package execution

import (
	"bytes"
	"context"
	_ "embed"
	"errors"
	"fmt"
	"grog/internal/config"
	"grog/internal/console"
	"grog/internal/logs"
	"grog/internal/model"
	"io"
	"os"
	"os/exec"
	"strings"
	"text/template"
	"time"
)

//go:embed run_sh.sh.tmpl
var binTemplate string

// templateData is the data expected by the run_sh.sh.tmpl.
type templateData struct {
	BinToolMap              BinToolMap
	OutputIdentifierMap     OutputIdentifierMap
	TransitiveOutputs       []string
	TransitiveTaggedOutputs TransitiveTaggedOutputs
	UserCommand             string
}

// extraArgsKey is the context key for extra command arguments passed via "--"
// on the grog test command line. These are forwarded to the target's shell
// command as positional parameters ($@).
type extraArgsKey struct{}

// WithExtraArgs returns a child context carrying extra command-line arguments
// that will be forwarded to every target command as shell positional parameters.
func WithExtraArgs(ctx context.Context, args []string) context.Context {
	return context.WithValue(ctx, extraArgsKey{}, args)
}

// ExtraArgsFromContext returns the extra command-line arguments stored in ctx,
// or nil if none are set.
func ExtraArgsFromContext(ctx context.Context) []string {
	args, _ := ctx.Value(extraArgsKey{}).([]string)
	return args
}

// BinToolMap Maps target label to tool a binary path.
type BinToolMap map[string]string

// OutputIdentifierMap maps a target label (and its shorthands) to the list of
// output identifiers produced by that dependency. File and directory outputs
// resolve to absolute paths while other output types retain their identifiers.
// The order matches the dependency's output definition order and includes bin
// outputs as the final element if present.
type OutputIdentifierMap map[string][]string

// TransitiveTaggedOutputs maps a tag name to the deduplicated list of output
// identifiers from all transitive ancestors that carry that tag. This enables
// shell commands to query outputs by semantic role (e.g. "find-links") rather
// than by individual target labels.
type TransitiveTaggedOutputs map[string][]string

func executeTarget(
	ctx context.Context,
	target *model.Target,
	binToolPaths BinToolMap,
	outputIdentifiers OutputIdentifierMap,
	transitiveOutputs []string,
	taggedOutputs TransitiveTaggedOutputs,
	streamLogs bool,
) error {
	if target.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, target.Timeout)
		defer cancel()
	}

	cmdOut, err := runTargetCommand(ctx, target, binToolPaths, outputIdentifiers, transitiveOutputs, taggedOutputs, target.Command, streamLogs)

	if err != nil {
		if ctx.Err() != nil {
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				return fmt.Errorf("timeout after %s", target.Timeout)
			}
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

// runTargetCommand runs a single shell command in the context of a target.
func runTargetCommand(
	ctx context.Context,
	target *model.Target,
	binToolPaths BinToolMap,
	outputIdentifiers OutputIdentifierMap,
	transitiveOutputs []string,
	taggedOutputs TransitiveTaggedOutputs,
	command string,
	streamLogs bool,
) ([]byte, error) {
	executionPath := config.GetPathAbsoluteToWorkspaceRoot(target.Label.Package)
	templatedCommand, err := getCommand(binToolPaths, outputIdentifiers, transitiveOutputs, taggedOutputs, command)
	if err != nil {
		return nil, err
	}

	// Execute the rendered script as a file rather than `sh -c "<script>"`: a
	// single argv element is capped at MAX_ARG_STRLEN (128 KB), which the
	// per-dependency output prelude can exceed ("argument list too long").
	// `sh` reads the file as data, so it needs no execute bit.
	scriptPath, cleanup, err := writeCommandScript(templatedCommand)
	if err != nil {
		return nil, err
	}
	defer cleanup()

	// Extra args (from "grog test //target -- -k foo") follow the script path so
	// they expand to $@. With a script file $0 is the path, so no placeholder.
	shellArgs := append([]string{scriptPath}, ExtraArgsFromContext(ctx)...)
	cmd := exec.CommandContext(ctx, "sh", shellArgs...)
	cmd.WaitDelay = 1 * time.Second // cancellation grace time

	// Attach env variables to the existing environment
	cmd.Env = GetExtendedTargetEnv(ctx, target)
	cmd.Dir = executionPath

	targetLogs := logs.NewTargetLogFile(*target)
	logWriter, err := targetLogs.Open()
	if err != nil {
		return nil, err
	}
	defer logWriter.Close()

	var buffer bytes.Buffer
	var teaWriter *console.TeaWriter

	if program := console.GetTeaProgram(ctx); program != nil {
		toggle := console.GetStreamLogsToggle(ctx)
		if toggle != nil {
			teaWriter = console.NewTeaWriter(program)
			toggleWriter := console.NewStreamToggleWriter(teaWriter, toggle)
			multiOut := io.MultiWriter(logWriter, toggleWriter, &buffer)
			cmd.Stdout = multiOut
			cmd.Stderr = multiOut
		} else if streamLogs {
			teaWriter = console.NewTeaWriter(program)
			multiOut := io.MultiWriter(logWriter, teaWriter, &buffer)
			cmd.Stdout = multiOut
			cmd.Stderr = multiOut
		} else {
			multiOut := io.MultiWriter(logWriter, &buffer)
			cmd.Stdout = multiOut
			cmd.Stderr = multiOut
		}
	} else {
		multiOut := io.MultiWriter(logWriter, &buffer)
		cmd.Stdout = multiOut
		cmd.Stderr = multiOut
	}

	cmdErr := cmd.Run()
	// Emit any buffered partial line now that the stream has closed.
	if teaWriter != nil {
		teaWriter.Flush()
	}
	if cmdErr != nil {
		return buffer.Bytes(), cmdErr
	}
	return buffer.Bytes(), nil
}

// writeCommandScript writes the rendered shell script to a temp file and returns
// its path plus a cleanup func. Running a file avoids the per-argument size limit
// (MAX_ARG_STRLEN) that large dependency-output preludes can exceed under `sh -c`.
func writeCommandScript(script string) (string, func(), error) {
	noop := func() {}
	f, err := os.CreateTemp("", "grog-cmd-*.sh")
	if err != nil {
		return "", noop, fmt.Errorf("failed to create command script file: %w", err)
	}
	cleanup := func() { _ = os.Remove(f.Name()) }
	if _, err := f.WriteString(script); err != nil {
		_ = f.Close()
		cleanup()
		return "", noop, fmt.Errorf("failed to write command script file: %w", err)
	}
	if err := f.Close(); err != nil {
		cleanup()
		return "", noop, fmt.Errorf("failed to close command script file: %w", err)
	}
	return f.Name(), cleanup, nil
}

func GetExtendedTargetEnv(ctx context.Context, target *model.Target) []string {
	gitHash, err := config.GetGitHash()
	if err != nil {
		console.GetLogger(ctx).Debugf("failed to get git hash: %v", err)
	}

	env := append([]string{}, os.Environ()...)
	for k, v := range config.Global.EnvironmentVariables {
		env = append(env, fmt.Sprintf("%s=%s", k, v))
	}
	for k, v := range target.EnvironmentVariables {
		env = append(env, fmt.Sprintf("%s=%s", k, v))
	}

	return append(env,
		"GROG_TARGET="+target.Label.String(),
		"GROG_OS="+config.Global.OS,
		"GROG_ARCH="+config.Global.Arch,
		"GROG_PLATFORM="+config.Global.GetPlatform(),
		"GROG_PLATFORM_TAGS="+strings.Join(config.Global.PlatformTags, ","),
		"GROG_PACKAGE="+target.Label.Package,
		"GROG_WORKSPACE_ROOT="+config.Global.WorkspaceRoot,
		"GROG_GIT_HASH="+gitHash,
	)
}

func getCommand(toolMap BinToolMap, outputMap OutputIdentifierMap, transitiveOutputs []string, taggedOutputs TransitiveTaggedOutputs, command string) (string, error) {
	tmpl, err := template.New("binCommand").Parse(binTemplate)
	if err != nil {
		return "", fmt.Errorf("failed to parse run template: %w", err)
	}

	userCommand := command
	if !config.Global.DisableDefaultShellFlags {
		userCommand = fmt.Sprintf("set -eu\n%s", command)
	}

	data := templateData{
		BinToolMap:              toolMap,
		OutputIdentifierMap:     outputMap,
		TransitiveOutputs:       transitiveOutputs,
		TransitiveTaggedOutputs: taggedOutputs,
		UserCommand:             userCommand,
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("failed to execute bin function template: %w", err)
	}

	return buf.String(), nil
}
