package execution

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"maps"
	"os"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"

	"grog/internal/config"
	"grog/internal/console"
	"grog/internal/dag"
	"grog/internal/hashing"
	"grog/internal/logs"
	"grog/internal/model"
	"grog/internal/worker"
)

// ResourceManager starts resource nodes lazily and at most once per
// invocation: the first executing target that depends on a resource triggers
// its up command, targets that are fully cached never do. Started resources
// are torn down in reverse start order when the build finishes.
type ResourceManager struct {
	startGroup singleflight.Group

	mutex sync.Mutex
	// started holds resources in start order; a resource is registered before
	// its up command runs so that partial starts are still torn down.
	started []*model.Resource
	// exports holds the resolved KEY=VALUE environment pairs per resource,
	// including dynamic exports written by the up command.
	exports map[string][]string
	// dependencyExports holds the exports a resource inherits from the
	// resources it depends on. Kept separate from exports so that dependent
	// targets only ever see the exports of the resources they declare.
	dependencyExports map[string][]string
	// startResults memoizes the terminal outcome of each start attempt.
	// singleflight only dedupes calls that overlap in time, so consumers
	// reaching a resource after its start finished must be served from here to
	// keep the at-most-once guarantee.
	startResults map[string]error
}

func NewResourceManager() *ResourceManager {
	return &ResourceManager{
		exports:           make(map[string][]string),
		dependencyExports: make(map[string][]string),
		startResults:      make(map[string]error),
	}
}

// EnsureResourcesStarted starts every resource the target directly depends on
// (and, transitively, the resources those depend on) unless already running,
// and returns the exported environment of the target's direct resources.
func (m *ResourceManager) EnsureResourcesStarted(
	ctx context.Context,
	graph *dag.DirectedTargetGraph,
	target *model.Target,
	update worker.StatusFunc,
) ([]string, error) {
	var combinedExports []string
	for _, dependency := range graph.GetDependencies(target) {
		resource, ok := dependency.(*model.Resource)
		if !ok {
			continue
		}

		update(worker.Status(fmt.Sprintf("%s: waiting for resource %s", target.Label, resource.Label)))
		if err := m.ensureStarted(ctx, graph, resource); err != nil {
			return nil, fmt.Errorf("resource %s required by %s: %w", resource.Label, target.Label, err)
		}

		m.mutex.Lock()
		combinedExports = append(combinedExports, m.exports[resource.Label.String()]...)
		m.mutex.Unlock()
	}
	return combinedExports, nil
}

// ensureStarted starts the resource exactly once for the whole invocation:
// singleflight collapses concurrent callers and startResults serves callers
// that arrive after the start already finished.
func (m *ResourceManager) ensureStarted(
	ctx context.Context,
	graph *dag.DirectedTargetGraph,
	resource *model.Resource,
) error {
	resourceLabel := resource.Label.String()
	if startErr, isStarted := m.startResult(resourceLabel); isStarted {
		return startErr
	}

	_, err, _ := m.startGroup.Do(resourceLabel, func() (any, error) {
		// A start may have completed between the check above and this call
		// acquiring the singleflight slot.
		if startErr, isStarted := m.startResult(resourceLabel); isStarted {
			return nil, startErr
		}

		startErr := m.startWithDependencies(ctx, graph, resource)

		m.mutex.Lock()
		m.startResults[resourceLabel] = startErr
		m.mutex.Unlock()

		return nil, startErr
	})
	return err
}

func (m *ResourceManager) startResult(resourceLabel string) (error, bool) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	startErr, isStarted := m.startResults[resourceLabel]
	return startErr, isStarted
}

// startWithDependencies starts the resources this resource depends on before
// registering and starting it, so that a resource always comes up after the
// resources it needs and is torn down before them.
func (m *ResourceManager) startWithDependencies(
	ctx context.Context,
	graph *dag.DirectedTargetGraph,
	resource *model.Resource,
) error {
	var dependencyExports []string
	for _, dependency := range graph.GetDependencies(resource) {
		dependencyResource, isResource := dependency.(*model.Resource)
		if !isResource {
			continue
		}

		if err := m.ensureStarted(ctx, graph, dependencyResource); err != nil {
			return fmt.Errorf("resource dependency %s: %w", dependencyResource.Label, err)
		}

		m.mutex.Lock()
		dependencyExports = append(dependencyExports, m.exports[dependencyResource.Label.String()]...)
		m.mutex.Unlock()
	}

	m.mutex.Lock()
	m.started = append(m.started, resource)
	m.dependencyExports[resource.Label.String()] = dependencyExports
	m.mutex.Unlock()

	return m.start(ctx, resource, dependencyExports)
}

func (m *ResourceManager) start(ctx context.Context, resource *model.Resource, dependencyExports []string) error {
	logger := console.GetLogger(ctx)
	startTime := time.Now()

	startContext, cancel := context.WithTimeout(ctx, resource.GetTimeout())
	defer cancel()

	exportsFile, err := os.CreateTemp("", "grog-resource-exports-*")
	if err != nil {
		return fmt.Errorf("failed to create exports file: %w", err)
	}
	exportsFilePath := exportsFile.Name()
	_ = exportsFile.Close()
	defer os.Remove(exportsFilePath)

	baseEnvironment := append(m.resourceEnv(resource), dependencyExports...)
	upEnvironment := append(append([]string{}, baseEnvironment...), "GROG_RESOURCE_EXPORTS_FILE="+exportsFilePath)

	output, err := m.runHookCommand(startContext, resource, resource.Up, upEnvironment)
	if err != nil {
		return fmt.Errorf("up command failed: %w\noutput: %s", err, string(output))
	}

	resolvedExports, err := m.resolveExports(resource, exportsFilePath)
	if err != nil {
		return err
	}

	// Publish exports before probing readiness: a resource whose up succeeded
	// but whose ready probe timed out still has to be torn down, and its down
	// command may need the dynamically exported container id or port.
	m.mutex.Lock()
	m.exports[resource.Label.String()] = resolvedExports
	m.mutex.Unlock()

	if resource.Ready != "" {
		readyEnvironment := append(append([]string{}, baseEnvironment...), resolvedExports...)
		if err := m.pollReady(startContext, resource, readyEnvironment); err != nil {
			return err
		}
	}

	if config.Global.DisableNonDeterministicLogging {
		logger.Infof("Resource %s started.", resource.Label)
	} else {
		logger.Infof("Resource %s started in %.1fs.", resource.Label, time.Since(startTime).Seconds())
	}
	return nil
}

// pollReady runs the ready probe until it succeeds or the start deadline is hit.
func (m *ResourceManager) pollReady(ctx context.Context, resource *model.Resource, environment []string) error {
	var lastOutput []byte
	var lastErr error
	for {
		lastOutput, lastErr = m.runHookCommand(ctx, resource, resource.Ready, environment)
		if lastErr == nil {
			return nil
		}

		select {
		case <-ctx.Done():
			return fmt.Errorf("did not become ready within %s: %w\nlast probe output: %s",
				resource.GetTimeout(), lastErr, string(lastOutput))
		case <-time.After(250 * time.Millisecond):
		}
	}
}

// TeardownAll runs the down command of every started resource in reverse
// start order. Failures are logged as warnings and do not fail the build.
// Idempotent: a second call is a no-op.
func (m *ResourceManager) TeardownAll(ctx context.Context) {
	m.mutex.Lock()
	started := m.started
	m.started = nil
	m.mutex.Unlock()

	logger := console.GetLogger(ctx)
	for i := len(started) - 1; i >= 0; i-- {
		resource := started[i]
		if resource.Down == "" {
			continue
		}

		downContext, cancel := context.WithTimeout(ctx, resource.GetTimeout())
		m.mutex.Lock()
		environment := append(m.resourceEnv(resource), m.dependencyExports[resource.Label.String()]...)
		environment = append(environment, m.exports[resource.Label.String()]...)
		m.mutex.Unlock()

		output, err := m.runHookCommand(downContext, resource, resource.Down, environment)
		cancel()
		if err != nil {
			logger.Warnf("Resource %s down command failed: %v\noutput: %s", resource.Label, err, string(output))
			continue
		}
		logger.Infof("Resource %s stopped.", resource.Label)
	}
}

// runHookCommand executes a resource lifecycle command in the resource's
// package directory and returns the combined output.
func (m *ResourceManager) runHookCommand(
	ctx context.Context,
	resource *model.Resource,
	command string,
	environment []string,
) ([]byte, error) {
	script := command
	if !config.Global.DisableDefaultShellFlags {
		script = "set -eu\n" + command
	}

	scriptPath, cleanup, err := writeCommandScript(script)
	if err != nil {
		return nil, err
	}
	defer cleanup()

	cmd := exec.CommandContext(ctx, "sh", scriptPath)
	cmd.WaitDelay = 1 * time.Second
	cmd.Dir = config.GetPathAbsoluteToWorkspaceRoot(resource.Label.Package)
	cmd.Env = environment

	resourceLogs := logs.NewTargetLogFile(model.Target{Label: resource.Label})
	var buffer bytes.Buffer
	if logWriter, logErr := resourceLogs.Open(); logErr == nil {
		defer logWriter.Close()
		multiOut := io.MultiWriter(&buffer, logWriter)
		cmd.Stdout = multiOut
		cmd.Stderr = multiOut
	} else {
		cmd.Stdout = &buffer
		cmd.Stderr = &buffer
	}

	if err := cmd.Run(); err != nil {
		return buffer.Bytes(), err
	}
	return buffer.Bytes(), nil
}

// resolveExports merges the statically declared exports with KEY=VALUE lines
// the up command appended to the exports file (dynamic values win) and
// returns them as a sorted environment slice.
func (m *ResourceManager) resolveExports(resource *model.Resource, exportsFilePath string) ([]string, error) {
	merged := make(map[string]string, len(resource.Exports))
	maps.Copy(merged, resource.Exports)

	content, err := os.ReadFile(exportsFilePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read exports file: %w", err)
	}
	for line := range strings.SplitSeq(string(content), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, found := strings.Cut(line, "=")
		if !found || key == "" {
			return nil, fmt.Errorf("resource %s wrote an invalid exports line (want KEY=VALUE): %q", resource.Label, line)
		}
		merged[key] = value
	}

	keys := make([]string, 0, len(merged))
	for key := range merged {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	environment := make([]string, 0, len(keys))
	for _, key := range keys {
		environment = append(environment, key+"="+merged[key])
	}
	return environment, nil
}

// resourceEnv builds the base environment for resource lifecycle commands.
func (m *ResourceManager) resourceEnv(resource *model.Resource) []string {
	environment := append([]string{}, os.Environ()...)
	for key, value := range config.Global.EnvironmentVariables {
		environment = append(environment, key+"="+value)
	}

	return append(environment,
		"GROG_RESOURCE="+resource.Label.String(),
		"GROG_RESOURCE_ID="+hashing.GetResourceIdentity(*resource),
		"GROG_OS="+config.Global.OS,
		"GROG_ARCH="+config.Global.Arch,
		"GROG_PLATFORM="+config.Global.GetPlatform(),
		"GROG_PACKAGE="+resource.Label.Package,
		"GROG_WORKSPACE_ROOT="+config.Global.WorkspaceRoot,
	)
}
