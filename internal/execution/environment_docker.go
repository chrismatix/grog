package execution

import (
	"context"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

// runDockerPhase implements the up and down phases of builtin::docker: up
// makes sure the image exists locally, down removes containers of this
// invocation that a killed exec phase left behind.
func runDockerPhase(ctx context.Context, start *environmentStart, phase string) ([]byte, error) {
	image := start.environment.Config["image"]
	switch phase {
	case "up":
		if output, err := exec.CommandContext(ctx, "docker", "image", "inspect", image).CombinedOutput(); err == nil {
			return output, nil
		}
		return exec.CommandContext(ctx, "docker", "pull", image).CombinedOutput()
	case "down":
		output, err := exec.CommandContext(ctx, "docker", "ps", "--all", "--quiet", "--filter", "label="+dockerInvocationLabel(start)).Output()
		containerIdentifiers := strings.Fields(string(output))
		if err != nil || len(containerIdentifiers) == 0 {
			return output, err
		}
		return exec.CommandContext(ctx, "docker", append([]string{"rm", "--force"}, containerIdentifiers...)...).CombinedOutput()
	}
	return nil, nil
}

// dockerRunCommand runs the action in a fresh container that mounts the input
// root at the same path, so absolute workspace paths stay valid inside it.
func dockerRunCommand(ctx context.Context, start *environmentStart, action environmentAction) *exec.Cmd {
	arguments := []string{
		"run", "--rm", "--init",
		"--label", dockerInvocationLabel(start),
		"--volume", action.InputRoot + ":" + action.InputRoot,
		"--workdir", filepath.Join(action.InputRoot, action.WorkingDirectory),
	}
	if userIdentifier := os.Getuid(); userIdentifier >= 0 {
		arguments = append(arguments, "--user", strconv.Itoa(userIdentifier)+":"+strconv.Itoa(os.Getgid()))
	}
	for _, key := range slices.Sorted(maps.Keys(action.EnvironmentVariables)) {
		arguments = append(arguments, "--env", key+"="+action.EnvironmentVariables[key])
	}
	arguments = append(arguments, start.environment.Config["image"])
	arguments = append(arguments, action.Argv...)

	command := exec.CommandContext(ctx, "docker", arguments...)
	stopProcessGroupOnCancel(command)
	return command
}

func dockerInvocationLabel(start *environmentStart) string {
	return "build.grog.invocation=" + start.invocationIdentifier
}
