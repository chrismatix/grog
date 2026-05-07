package execution

import (
	"bytes"
	"context"
	"fmt"
	"grog/internal/config"
	"grog/internal/console"
	"grog/internal/logs"
	"grog/internal/model"
	"io"
	"os"
	"os/exec"
	"os/user"
	"time"
)

const containerWorkspaceRoot = "/workspace"

// HasDockerTargets returns true if any selected target in the graph references
// a Docker environment. Used to check Docker availability only when needed.
func HasDockerTargets(graph interface{ GetSelectedNodes() []model.BuildNode }) bool {
	for _, node := range graph.GetSelectedNodes() {
		if target, ok := node.(*model.Target); ok && target.Environment != nil {
			return true
		}
	}
	return false
}

// CheckDockerAvailable verifies that the Docker daemon is accessible.
func CheckDockerAvailable() error {
	cmd := exec.Command("docker", "info")
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker is not available (required for sandboxed targets): %w", err)
	}
	return nil
}

// runTargetCommandInDocker runs a target's command inside a Docker container
// using the specified environment configuration.
func runTargetCommandInDocker(
	ctx context.Context,
	target *model.Target,
	env *model.Environment,
	binToolPaths BinToolMap,
	outputIdentifiers OutputIdentifierMap,
	command string,
	streamLogs bool,
) ([]byte, error) {
	templatedCommand, err := getCommand(binToolPaths, outputIdentifiers, command)
	if err != nil {
		return nil, err
	}

	workspaceRoot := config.Global.WorkspaceRoot
	packagePath := target.Label.Package
	containerWorkDir := containerWorkspaceRoot
	if packagePath != "" {
		containerWorkDir = containerWorkspaceRoot + "/" + packagePath
	}

	// Build docker run arguments
	args := []string{
		"run", "--rm",
	}

	// Run as the current user to avoid permission issues with mounted outputs
	if u, err := user.Current(); err == nil {
		args = append(args, "--user", u.Uid+":"+u.Gid)
	}

	// Bind-mount the workspace root
	args = append(args, "-v", workspaceRoot+":"+containerWorkspaceRoot)

	// Set the working directory inside the container
	args = append(args, "-w", containerWorkDir)

	// Pass environment variables, overriding GROG_WORKSPACE_ROOT for the container
	for _, envVar := range getDockerTargetEnv(ctx, target) {
		args = append(args, "-e", envVar)
	}

	// Image and command
	args = append(args, env.DockerImage, "sh", "-c", templatedCommand)

	cmd := exec.CommandContext(ctx, "docker", args...)
	cmd.WaitDelay = 1 * time.Second

	targetLogs := logs.NewTargetLogFile(*target)
	logWriter, err := targetLogs.Open()
	if err != nil {
		return nil, err
	}
	defer logWriter.Close()

	var buffer bytes.Buffer

	if program := console.GetTeaProgram(ctx); program != nil {
		toggle := console.GetStreamLogsToggle(ctx)
		if toggle != nil {
			teaWriter := console.NewTeaWriter(program)
			toggleWriter := console.NewStreamToggleWriter(teaWriter, toggle)
			multiOut := io.MultiWriter(logWriter, toggleWriter, &buffer)
			cmd.Stdout = multiOut
			cmd.Stderr = multiOut
		} else if streamLogs {
			teaWriter := console.NewTeaWriter(program)
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

	if cmdErr := cmd.Run(); cmdErr != nil {
		return buffer.Bytes(), cmdErr
	}
	return buffer.Bytes(), nil
}

// getDockerTargetEnv returns environment variables for Docker execution.
// It mirrors GetExtendedTargetEnv but sets GROG_WORKSPACE_ROOT to the
// container mount point and adds GROG_SANDBOXED=true.
func getDockerTargetEnv(ctx context.Context, target *model.Target) []string {
	gitHash, err := config.GetGitHash()
	if err != nil {
		console.GetLogger(ctx).Debugf("failed to get git hash: %v", err)
	}

	// Start with global and target-specific environment variables only.
	// We don't inherit os.Environ() since the container has its own base env.
	var env []string
	for k, v := range config.Global.EnvironmentVariables {
		env = append(env, fmt.Sprintf("%s=%s", k, v))
	}
	for k, v := range target.EnvironmentVariables {
		env = append(env, fmt.Sprintf("%s=%s", k, v))
	}

	// Remap workspace root and add GROG_* variables
	env = append(env,
		"GROG_TARGET="+target.Label.String(),
		"GROG_OS="+config.Global.OS,
		"GROG_ARCH="+config.Global.Arch,
		"GROG_PLATFORM="+config.Global.GetPlatform(),
		"GROG_PACKAGE="+target.Label.Package,
		"GROG_WORKSPACE_ROOT="+containerWorkspaceRoot,
		"GROG_GIT_HASH="+gitHash,
		"GROG_SANDBOXED=true",
	)

	// Also pass through HOME so tools that need it work
	if home := os.Getenv("HOME"); home != "" {
		env = append(env, "HOME="+home)
	}

	return env
}
