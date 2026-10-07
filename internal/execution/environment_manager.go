package execution

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"

	"grog/internal/config"
	"grog/internal/console"
	"grog/internal/logs"
	"grog/internal/model"
	"grog/internal/output/handlers"
	"grog/internal/shell"
)

const (
	environmentProtocolVersion = "1"
	// environmentFailureExitCode is what a provider's exec phase exits with when
	// the provider itself failed rather than the command (docker run's convention).
	environmentFailureExitCode = 125
	environmentStopGracePeriod = 10 * time.Second
)

// EnvironmentManager starts environments lazily and at most once per
// invocation: the first executing target in an environment triggers the
// provider's up phase, fully cached targets never do. Started environments
// are torn down in reverse start order when the build finishes.
type EnvironmentManager struct {
	invocationIdentifier string

	mutex       sync.Mutex
	tearingDown bool
	starts      map[string]*environmentStart
	// started holds starts in start order. A start is registered before up
	// runs so that partial starts are still torn down.
	started []*environmentStart
}

// environmentStart is the lifecycle of one environment within an invocation.
type environmentStart struct {
	environment          *model.Environment
	invocationIdentifier string
	configFilePath       string
	// done is closed once up has finished; err and state are set before.
	done chan struct{}
	err  error
	// state holds the KEY=VALUE pairs up wrote to $GROG_ENV_STATE_FILE.
	state []string
}

// environmentAction is the document the exec phase reads from $GROG_ACTION_FILE.
type environmentAction struct {
	Target               string            `json:"target"`
	Argv                 []string          `json:"argv"`
	WorkingDirectory     string            `json:"working_directory"`
	EnvironmentVariables map[string]string `json:"environment_variables"`
	InputRoot            string            `json:"input_root"`
	OutputPaths          []string          `json:"output_paths"`
}

func NewEnvironmentManager() *EnvironmentManager {
	identifier := make([]byte, 8)
	_, _ = rand.Read(identifier)
	return &EnvironmentManager{
		invocationIdentifier: hex.EncodeToString(identifier),
		starts:               make(map[string]*environmentStart),
	}
}

// EnsureStarted runs the environment's up phase unless it already ran in this
// invocation. Concurrent callers wait for the same start.
func (m *EnvironmentManager) EnsureStarted(ctx context.Context, environment *model.Environment) (*environmentStart, error) {
	m.mutex.Lock()
	if m.tearingDown {
		m.mutex.Unlock()
		return nil, context.Canceled
	}
	start, isStarting := m.starts[environment.Label.String()]
	if !isStarting {
		start = &environmentStart{
			environment:          environment,
			invocationIdentifier: m.invocationIdentifier,
			done:                 make(chan struct{}),
		}
		m.starts[environment.Label.String()] = start
		m.started = append(m.started, start)
	}
	m.mutex.Unlock()

	if isStarting {
		select {
		case <-start.done:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	} else {
		start.err = start.up(ctx)
		close(start.done)
	}

	if start.err != nil {
		return nil, fmt.Errorf("environment %s: %w", environment.Label, start.err)
	}
	return start, nil
}

// TeardownAll runs the down phase of every started environment in reverse
// start order. Failures are logged as warnings and do not fail the build.
func (m *EnvironmentManager) TeardownAll(ctx context.Context) {
	m.mutex.Lock()
	m.tearingDown = true
	started := m.started
	m.started = nil
	m.mutex.Unlock()

	logger := console.GetLogger(ctx)
	for _, start := range slices.Backward(started) {
		<-start.done

		downContext, cancel := context.WithTimeout(ctx, start.environment.GetTimeout())
		output, err := start.runPhase(downContext, "down")
		cancel()
		_ = os.Remove(start.configFilePath)
		if err != nil {
			logger.Warnf("Environment %s down phase failed: %v\noutput: %s", start.environment.Label, err, string(output))
			continue
		}
		logger.Infof("Environment %s stopped.", start.environment.Label)
	}
}

func (s *environmentStart) up(ctx context.Context) error {
	startTime := time.Now()
	upContext, cancel := context.WithTimeout(ctx, s.environment.GetTimeout())
	defer cancel()

	configuration := s.environment.Config
	if configuration == nil {
		configuration = map[string]string{}
	}
	configFilePath, err := writeTemporaryJSON("grog-env-config-*.json", configuration)
	if err != nil {
		return err
	}
	s.configFilePath = configFilePath

	stateFile, err := os.CreateTemp("", "grog-env-state-*")
	if err != nil {
		return fmt.Errorf("failed to create state file: %w", err)
	}
	stateFilePath := stateFile.Name()
	_ = stateFile.Close()
	defer func() { _ = os.Remove(stateFilePath) }()

	output, err := s.runPhase(upContext, "up", "GROG_ENV_STATE_FILE="+stateFilePath)
	if err != nil {
		return fmt.Errorf("up phase failed: %w\noutput: %s", err, string(output))
	}

	state, err := readKeyValueFile(stateFilePath)
	if err != nil {
		return fmt.Errorf("state file: %w", err)
	}
	s.state = exportsEnvironment(state)

	logger := console.GetLogger(ctx)
	if config.Global.DisableNonDeterministicLogging {
		logger.Infof("Environment %s started.", s.environment.Label)
	} else {
		logger.Infof("Environment %s started in %.1fs.", s.environment.Label, time.Since(startTime).Seconds())
	}
	return nil
}

// runPhase runs the provider's up or down phase and returns its combined output.
func (s *environmentStart) runPhase(ctx context.Context, phase string, extraEnvironment ...string) (output []byte, err error) {
	if s.environment.Provider == model.DockerProvider {
		return runDockerPhase(ctx, s, phase)
	}

	command, cleanup, err := s.providerCommand(ctx, phase, extraEnvironment...)
	if err != nil {
		return nil, err
	}
	defer cleanup()

	var buffer bytes.Buffer
	environmentLogs := logs.NewTargetLogFile(model.Target{Label: s.environment.Label})
	if logWriter, logErr := environmentLogs.Open(); logErr == nil {
		defer func() { err = errors.Join(err, logWriter.Close()) }()
		command.Stdout = io.MultiWriter(&buffer, logWriter)
	} else {
		command.Stdout = &buffer
	}
	command.Stderr = command.Stdout

	err = command.Run()
	return buffer.Bytes(), err
}

// execCommand returns the command that runs argv for the target inside the
// environment. commandEnvironment is the complete environment of the command;
// the host environment only reaches the provider process.
func (s *environmentStart) execCommand(
	ctx context.Context,
	target *model.Target,
	argv []string,
	commandEnvironment []string,
) (*exec.Cmd, func(), error) {
	workingDirectory := target.Label.Package
	if workingDirectory == "" {
		workingDirectory = "."
	}
	action := environmentAction{
		Target:               target.Label.String(),
		Argv:                 argv,
		WorkingDirectory:     workingDirectory,
		EnvironmentVariables: make(map[string]string, len(commandEnvironment)),
		InputRoot:            config.Global.WorkspaceRoot,
		OutputPaths:          []string{},
	}
	for _, entry := range commandEnvironment {
		key, value, _ := strings.Cut(entry, "=")
		action.EnvironmentVariables[key] = value
	}
	for _, targetOutput := range target.AllOutputs() {
		if targetOutput.Type == string(handlers.FileHandler) || targetOutput.Type == string(handlers.DirHandler) {
			action.OutputPaths = append(action.OutputPaths, filepath.Join(target.Label.Package, targetOutput.Identifier))
		}
	}

	if s.environment.Provider == model.DockerProvider {
		return dockerRunCommand(ctx, s, action), func() {}, nil
	}

	actionFilePath, err := writeTemporaryJSON("grog-env-action-*.json", action)
	if err != nil {
		return nil, nil, err
	}
	command, cleanup, err := s.providerCommand(ctx, "exec", "GROG_ACTION_FILE="+actionFilePath)
	if err != nil {
		_ = os.Remove(actionFilePath)
		return nil, nil, err
	}
	return command, func() {
		cleanup()
		_ = os.Remove(actionFilePath)
	}, nil
}

// providerCommand prepares a call of the provider executable for one phase.
func (s *environmentStart) providerCommand(ctx context.Context, phase string, extraEnvironment ...string) (*exec.Cmd, func(), error) {
	command, cleanup, err := shell.NewCommand(ctx, shell.WithDefaultFlags(s.environment.Provider))
	if err != nil {
		return nil, nil, err
	}
	command.Dir = config.GetPathAbsoluteToWorkspaceRoot(s.environment.Label.Package)

	command.Env = append([]string{}, os.Environ()...)
	for key, value := range config.Global.EnvironmentVariables {
		command.Env = append(command.Env, key+"="+value)
	}
	command.Env = append(command.Env,
		"GROG_ENV_PHASE="+phase,
		"GROG_ENV_PROTOCOL="+environmentProtocolVersion,
		"GROG_ENV="+s.environment.Label.String(),
		"GROG_ENV_ID="+s.identifier(),
		"GROG_INVOCATION_ID="+s.invocationIdentifier,
		"GROG_ENV_CONFIG_FILE="+s.configFilePath,
		"GROG_OS="+config.Global.OS,
		"GROG_ARCH="+config.Global.Arch,
		"GROG_PLATFORM="+config.Global.GetPlatform(),
		"GROG_PACKAGE="+s.environment.Label.Package,
		"GROG_WORKSPACE_ROOT="+config.Global.WorkspaceRoot,
	)
	command.Env = append(command.Env, s.state...)
	command.Env = append(command.Env, extraEnvironment...)

	stopProcessGroupOnCancel(command)
	return command, cleanup, nil
}

// identifier is a short id for naming containers or VMs that stays stable
// across runs as long as the environment does not change.
func (s *environmentStart) identifier() string {
	return s.environment.IdentityHash[:min(12, len(s.environment.IdentityHash))]
}

// stopProcessGroupOnCancel sends SIGTERM to the command's whole process group
// on cancellation, so that a provider script and the tools it started can stop
// remote work, and kills the command after a grace period.
func stopProcessGroupOnCancel(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	command.Cancel = func() error {
		return syscall.Kill(-command.Process.Pid, syscall.SIGTERM)
	}
	command.WaitDelay = environmentStopGracePeriod
}

func writeTemporaryJSON(pattern string, value any) (string, error) {
	content, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	file, err := os.CreateTemp("", pattern)
	if err != nil {
		return "", fmt.Errorf("failed to create %s: %w", pattern, err)
	}
	_, writeErr := file.Write(content)
	if err := errors.Join(writeErr, file.Close()); err != nil {
		_ = os.Remove(file.Name())
		return "", fmt.Errorf("failed to write %s: %w", file.Name(), err)
	}
	return file.Name(), nil
}
