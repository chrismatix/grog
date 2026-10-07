package execution

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"grog/internal/label"
	"grog/internal/model"
)

func newTestEnvironment(t *testing.T, workspaceRoot string, name string, providerScript string) *model.Environment {
	t.Helper()
	providerFile := name + "_provider.sh"
	if err := os.WriteFile(filepath.Join(workspaceRoot, "pkg", providerFile), []byte(providerScript), 0755); err != nil {
		t.Fatalf("failed to write provider: %v", err)
	}
	return &model.Environment{
		Label:        label.TargetLabel{Package: "pkg", Name: name},
		Provider:     "sh " + providerFile,
		Config:       map[string]string{"image": "builder:1"},
		IdentityHash: "0123456789abcdef0123",
	}
}

func readLines(t *testing.T, filePath string) []string {
	t.Helper()
	content, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("failed to read %s: %v", filePath, err)
	}
	return strings.Fields(string(content))
}

func TestEnvironmentEnsureStartedRunsUpOnce(t *testing.T) {
	workspaceRoot := setupResourceTestWorkspace(t)
	phaseLog := filepath.Join(workspaceRoot, "phases.log")

	environment := newTestEnvironment(t, workspaceRoot, "env", `
echo "$GROG_ENV_PHASE" >> `+phaseLog+`
if [ "$GROG_ENV_PHASE" = up ]; then echo "SESSION=from-up" >> "$GROG_ENV_STATE_FILE"; fi
`)

	manager := NewEnvironmentManager()
	var waitGroup sync.WaitGroup
	starts := make([]*environmentStart, 10)
	errors := make([]error, 10)
	for index := range 10 {
		waitGroup.Go(func() {
			starts[index], errors[index] = manager.EnsureStarted(context.Background(), environment)
		})
	}
	waitGroup.Wait()

	for _, err := range errors {
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
	}
	if phases := readLines(t, phaseLog); !reflect.DeepEqual(phases, []string{"up"}) {
		t.Fatalf("expected exactly one up phase, got %v", phases)
	}
	if !reflect.DeepEqual(starts[0].state, []string{"SESSION=from-up"}) {
		t.Fatalf("expected state from the up phase, got %v", starts[0].state)
	}

	manager.TeardownAll(context.Background())
	if phases := readLines(t, phaseLog); !reflect.DeepEqual(phases, []string{"up", "down"}) {
		t.Fatalf("expected up then down, got %v", phases)
	}
}

func TestEnvironmentExecCommandHandsTheActionToTheProvider(t *testing.T) {
	workspaceRoot := setupResourceTestWorkspace(t)
	capturedAction := filepath.Join(workspaceRoot, "action.json")
	capturedProviderEnvironment := filepath.Join(workspaceRoot, "provider.env")

	environment := newTestEnvironment(t, workspaceRoot, "env", `
case "$GROG_ENV_PHASE" in
  up) echo "SESSION=from-up" >> "$GROG_ENV_STATE_FILE" ;;
  exec)
    cp "$GROG_ACTION_FILE" `+capturedAction+`
    echo "$GROG_ENV_PROTOCOL $GROG_ENV $GROG_ENV_ID $SESSION $(cat "$GROG_ENV_CONFIG_FILE")" > `+capturedProviderEnvironment+`
    ;;
esac
`)
	target := &model.Target{
		Label: label.TargetLabel{Package: "pkg", Name: "app"},
		Outputs: []model.Output{
			{Type: "file", Identifier: "dist/app"},
			{Type: "dir", Identifier: "dist/assets/"},
			{Type: "oci", Identifier: "app:latest"},
		},
	}

	manager := NewEnvironmentManager()
	start, err := manager.EnsureStarted(context.Background(), environment)
	if err != nil {
		t.Fatalf("failed to start environment: %v", err)
	}
	command, cleanup, err := start.execCommand(context.Background(), target, []string{"sh", "-c", "make", "sh"}, []string{"GROG_TARGET=//pkg:app", "TOKEN=a=b"})
	if err != nil {
		t.Fatalf("failed to create exec command: %v", err)
	}
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("exec phase failed: %v\n%s", err, output)
	}
	cleanup()

	content, err := os.ReadFile(capturedAction)
	if err != nil {
		t.Fatalf("failed to read captured action: %v", err)
	}
	var action environmentAction
	if err := json.Unmarshal(content, &action); err != nil {
		t.Fatalf("failed to decode action: %v", err)
	}
	expectedAction := environmentAction{
		Target:               "//pkg:app",
		Argv:                 []string{"sh", "-c", "make", "sh"},
		WorkingDirectory:     "pkg",
		EnvironmentVariables: map[string]string{"GROG_TARGET": "//pkg:app", "TOKEN": "a=b"},
		InputRoot:            workspaceRoot,
		OutputPaths:          []string{"pkg/dist/app", "pkg/dist/assets"},
	}
	if !reflect.DeepEqual(action, expectedAction) {
		t.Fatalf("unexpected action\n got: %+v\nwant: %+v", action, expectedAction)
	}

	providerEnvironment, err := os.ReadFile(capturedProviderEnvironment)
	if err != nil {
		t.Fatalf("failed to read provider environment: %v", err)
	}
	if got, want := strings.TrimSpace(string(providerEnvironment)), `1 //pkg:env 0123456789ab from-up {"image":"builder:1"}`; got != want {
		t.Fatalf("unexpected provider environment\n got: %s\nwant: %s", got, want)
	}
}

func TestEnvironmentTeardownAfterFailedUp(t *testing.T) {
	workspaceRoot := setupResourceTestWorkspace(t)
	phaseLog := filepath.Join(workspaceRoot, "phases.log")

	healthy := newTestEnvironment(t, workspaceRoot, "healthy", `echo "$GROG_ENV_PHASE-$GROG_ENV" >> `+phaseLog)
	broken := newTestEnvironment(t, workspaceRoot, "broken", `
echo "$GROG_ENV_PHASE-$GROG_ENV" >> `+phaseLog+`
if [ "$GROG_ENV_PHASE" = up ]; then echo "image not found"; exit 1; fi
`)

	manager := NewEnvironmentManager()
	if _, err := manager.EnsureStarted(context.Background(), healthy); err != nil {
		t.Fatalf("failed to start healthy environment: %v", err)
	}
	for range 2 {
		_, err := manager.EnsureStarted(context.Background(), broken)
		if err == nil || !strings.Contains(err.Error(), "image not found") {
			t.Fatalf("expected the up failure for every caller, got %v", err)
		}
	}

	manager.TeardownAll(context.Background())
	expectedPhases := []string{"up-//pkg:healthy", "up-//pkg:broken", "down-//pkg:broken", "down-//pkg:healthy"}
	if phases := readLines(t, phaseLog); !reflect.DeepEqual(phases, expectedPhases) {
		t.Fatalf("expected a partial start to be torn down in reverse order\n got: %v\nwant: %v", phases, expectedPhases)
	}

	if _, err := manager.EnsureStarted(context.Background(), healthy); err == nil {
		t.Fatalf("expected starts after teardown to fail")
	}
}

func TestEnvironmentExecIsStoppedWithSigterm(t *testing.T) {
	workspaceRoot := setupResourceTestWorkspace(t)
	stoppedMarker := filepath.Join(workspaceRoot, "stopped")

	// The trap only fires if the whole process group receives SIGTERM: sh
	// runs the provider script as a child process.
	environment := newTestEnvironment(t, workspaceRoot, "env", `
if [ "$GROG_ENV_PHASE" = exec ]; then
  trap 'echo stopped > `+stoppedMarker+`; exit 143' TERM
  sleep 30 &
  wait
fi
`)

	manager := NewEnvironmentManager()
	start, err := manager.EnsureStarted(context.Background(), environment)
	if err != nil {
		t.Fatalf("failed to start environment: %v", err)
	}
	commandContext, cancel := context.WithCancel(context.Background())
	command, cleanup, err := start.execCommand(commandContext, &model.Target{Label: label.TargetLabel{Package: "pkg", Name: "app"}}, []string{"true"}, nil)
	if err != nil {
		t.Fatalf("failed to create exec command: %v", err)
	}
	defer cleanup()
	if err := command.Start(); err != nil {
		t.Fatalf("failed to start exec phase: %v", err)
	}
	time.Sleep(200 * time.Millisecond)
	cancel()
	_ = command.Wait()

	if _, err := os.Stat(stoppedMarker); err != nil {
		t.Fatalf("expected the provider to receive SIGTERM: %v", err)
	}
}

func TestDockerRunCommandMountsTheWorkspaceAtTheSamePath(t *testing.T) {
	start := &environmentStart{
		environment:          &model.Environment{Config: map[string]string{"image": "busybox:1.37"}},
		invocationIdentifier: "abc123",
	}
	action := environmentAction{
		Argv:                 []string{"sh", "-c", "make", "sh", "--verbose"},
		WorkingDirectory:     "services/api",
		EnvironmentVariables: map[string]string{"B": "2", "A": "1"},
		InputRoot:            "/work/repo",
	}

	arguments := dockerRunCommand(context.Background(), start, action).Args
	userIndex := slices.Index(arguments, "--user")
	if userIndex < 0 {
		t.Fatalf("expected the container to run as the host user, got %v", arguments)
	}
	arguments = slices.Delete(arguments, userIndex, userIndex+2)

	expected := []string{
		"docker", "run", "--rm", "--init",
		"--label", "build.grog.invocation=abc123",
		"--volume", "/work/repo:/work/repo",
		"--workdir", "/work/repo/services/api",
		"--env", "A=1",
		"--env", "B=2",
		"busybox:1.37",
		"sh", "-c", "make", "sh", "--verbose",
	}
	if !reflect.DeepEqual(arguments, expected) {
		t.Fatalf("unexpected docker arguments\n got: %v\nwant: %v", arguments, expected)
	}
}
