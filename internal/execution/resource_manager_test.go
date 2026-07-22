package execution

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"grog/internal/config"
	"grog/internal/dag"
	"grog/internal/label"
	"grog/internal/model"
	"grog/internal/worker"
)

func setupResourceTestWorkspace(t *testing.T) string {
	t.Helper()
	tmpDir := t.TempDir()

	prev := config.Global
	config.Global = config.WorkspaceConfig{
		Root:                     tmpDir,
		WorkspaceRoot:            tmpDir,
		DisableDefaultShellFlags: true,
	}
	t.Cleanup(func() { config.Global = prev })

	if err := os.MkdirAll(filepath.Join(tmpDir, "pkg"), 0755); err != nil {
		t.Fatalf("failed to create package dir: %v", err)
	}
	return tmpDir
}

func noopStatus(worker.StatusUpdate) {}

func newResourceTestGraph(t *testing.T, nodes ...model.BuildNode) *dag.DirectedTargetGraph {
	t.Helper()
	graph := dag.NewDirectedGraphFromTargets(nodes...)
	return graph
}

func TestEnsureResourcesStartedStartsOnceAndInjectsExports(t *testing.T) {
	tmpDir := setupResourceTestWorkspace(t)
	countFile := filepath.Join(tmpDir, "starts.log")

	resource := &model.Resource{
		Label: label.TargetLabel{Package: "pkg", Name: "db"},
		Up:    `echo up >> ` + countFile + `; echo "DYNAMIC_TOKEN=from-up" >> "$GROG_RESOURCE_EXPORTS_FILE"`,
		Exports: map[string]string{
			"STATIC_TOKEN": "from-static",
			// The up command overrides this one dynamically
			"DYNAMIC_TOKEN": "overridden",
		},
	}
	target := &model.Target{
		Label:        label.TargetLabel{Package: "pkg", Name: "consumer"},
		Dependencies: []label.TargetLabel{resource.Label},
	}
	graph := newResourceTestGraph(t, resource, target)
	if err := graph.AddEdge(resource, target); err != nil {
		t.Fatalf("failed to add edge: %v", err)
	}

	manager := NewResourceManager()

	var waitGroup sync.WaitGroup
	environments := make([][]string, 10)
	errors := make([]error, 10)
	for i := range 10 {
		waitGroup.Add(1)
		go func(index int) {
			defer waitGroup.Done()
			environments[index], errors[index] = manager.EnsureResourcesStarted(context.Background(), graph, target, noopStatus)
		}(i)
	}
	waitGroup.Wait()

	for _, err := range errors {
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
	}

	content, err := os.ReadFile(countFile)
	if err != nil {
		t.Fatalf("failed to read start counter: %v", err)
	}
	if got := strings.Count(string(content), "up"); got != 1 {
		t.Fatalf("expected exactly one up invocation, got %d", got)
	}

	joined := strings.Join(environments[0], "\n")
	if !strings.Contains(joined, "STATIC_TOKEN=from-static") {
		t.Errorf("expected static export, got %v", environments[0])
	}
	if !strings.Contains(joined, "DYNAMIC_TOKEN=from-up") {
		t.Errorf("expected dynamic export to win, got %v", environments[0])
	}
}

func TestEnsureResourcesStartedWaitsForReady(t *testing.T) {
	tmpDir := setupResourceTestWorkspace(t)
	readyFile := filepath.Join(tmpDir, "ready.marker")

	resource := &model.Resource{
		Label: label.TargetLabel{Package: "pkg", Name: "db"},
		// Up daemonizes a short task that only later creates the ready marker
		Up:    `(sleep 0.4 && touch ` + readyFile + `) &`,
		Ready: `test -f ` + readyFile,
	}
	target := &model.Target{
		Label:        label.TargetLabel{Package: "pkg", Name: "consumer"},
		Dependencies: []label.TargetLabel{resource.Label},
	}
	graph := newResourceTestGraph(t, resource, target)
	if err := graph.AddEdge(resource, target); err != nil {
		t.Fatalf("failed to add edge: %v", err)
	}

	manager := NewResourceManager()
	if _, err := manager.EnsureResourcesStarted(context.Background(), graph, target, noopStatus); err != nil {
		t.Fatalf("expected resource to become ready, got %v", err)
	}
	if _, err := os.Stat(readyFile); err != nil {
		t.Fatalf("expected ready marker to exist: %v", err)
	}
}

func TestEnsureResourcesStartedFailsWhenReadyTimesOut(t *testing.T) {
	setupResourceTestWorkspace(t)

	resource := &model.Resource{
		Label:   label.TargetLabel{Package: "pkg", Name: "db"},
		Up:      "true",
		Ready:   "false",
		Timeout: 500 * time.Millisecond,
	}
	target := &model.Target{
		Label:        label.TargetLabel{Package: "pkg", Name: "consumer"},
		Dependencies: []label.TargetLabel{resource.Label},
	}
	graph := newResourceTestGraph(t, resource, target)
	if err := graph.AddEdge(resource, target); err != nil {
		t.Fatalf("failed to add edge: %v", err)
	}

	manager := NewResourceManager()
	_, err := manager.EnsureResourcesStarted(context.Background(), graph, target, noopStatus)
	if err == nil {
		t.Fatal("expected ready timeout error")
	}
	if !strings.Contains(err.Error(), "did not become ready") {
		t.Fatalf("expected ready timeout error, got %v", err)
	}
}

func TestTeardownAllRunsDownInReverseStartOrder(t *testing.T) {
	tmpDir := setupResourceTestWorkspace(t)
	orderFile := filepath.Join(tmpDir, "order.log")

	first := &model.Resource{
		Label: label.TargetLabel{Package: "pkg", Name: "first"},
		Up:    "true",
		Down:  "echo first >> " + orderFile,
	}
	second := &model.Resource{
		Label: label.TargetLabel{Package: "pkg", Name: "second"},
		Up:    "true",
		Down:  "echo second >> " + orderFile,
	}
	target := &model.Target{
		Label:        label.TargetLabel{Package: "pkg", Name: "consumer"},
		Dependencies: []label.TargetLabel{first.Label, second.Label},
	}
	graph := newResourceTestGraph(t, first, second, target)
	if err := graph.AddEdge(first, target); err != nil {
		t.Fatalf("failed to add edge: %v", err)
	}
	if err := graph.AddEdge(second, target); err != nil {
		t.Fatalf("failed to add edge: %v", err)
	}

	manager := NewResourceManager()
	if _, err := manager.EnsureResourcesStarted(context.Background(), graph, target, noopStatus); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	manager.TeardownAll(context.Background())

	content, err := os.ReadFile(orderFile)
	if err != nil {
		t.Fatalf("failed to read teardown order: %v", err)
	}
	lines := strings.Fields(string(content))
	if len(lines) != 2 || lines[0] != "second" || lines[1] != "first" {
		t.Fatalf("expected reverse start order [second first], got %v", lines)
	}

	// TeardownAll is idempotent
	manager.TeardownAll(context.Background())
	content, _ = os.ReadFile(orderFile)
	if got := len(strings.Fields(string(content))); got != 2 {
		t.Fatalf("expected no additional teardowns, got %d lines", got)
	}
}

func TestTeardownAllRunsForPartiallyFailedStart(t *testing.T) {
	tmpDir := setupResourceTestWorkspace(t)
	downFile := filepath.Join(tmpDir, "down.marker")

	resource := &model.Resource{
		Label: label.TargetLabel{Package: "pkg", Name: "db"},
		Up:    "false",
		Down:  "touch " + downFile,
	}
	target := &model.Target{
		Label:        label.TargetLabel{Package: "pkg", Name: "consumer"},
		Dependencies: []label.TargetLabel{resource.Label},
	}
	graph := newResourceTestGraph(t, resource, target)
	if err := graph.AddEdge(resource, target); err != nil {
		t.Fatalf("failed to add edge: %v", err)
	}

	manager := NewResourceManager()
	if _, err := manager.EnsureResourcesStarted(context.Background(), graph, target, noopStatus); err == nil {
		t.Fatal("expected up failure")
	}

	manager.TeardownAll(context.Background())
	if _, err := os.Stat(downFile); err != nil {
		t.Fatalf("expected down to run for partially started resource: %v", err)
	}
}

func TestTeardownAfterFailedUpHasExports(t *testing.T) {
	tmpDir := setupResourceTestWorkspace(t)
	downFile := filepath.Join(tmpDir, "down.log")

	resource := &model.Resource{
		Label:   label.TargetLabel{Package: "pkg", Name: "db"},
		Up:      `echo "CONTAINER_ID=abc123" >> "$GROG_RESOURCE_EXPORTS_FILE"; false`,
		Down:    `echo "$STATIC_NAME:$CONTAINER_ID" > ` + downFile,
		Exports: map[string]string{"STATIC_NAME": "database"},
	}
	target := &model.Target{
		Label:        label.TargetLabel{Package: "pkg", Name: "consumer"},
		Dependencies: []label.TargetLabel{resource.Label},
	}
	graph := newResourceTestGraph(t, resource, target)
	if err := graph.AddEdge(resource, target); err != nil {
		t.Fatalf("failed to add edge: %v", err)
	}

	manager := NewResourceManager()
	if _, err := manager.EnsureResourcesStarted(context.Background(), graph, target, noopStatus); err == nil {
		t.Fatal("expected up failure")
	}
	manager.TeardownAll(context.Background())

	content, err := os.ReadFile(downFile)
	if err != nil {
		t.Fatalf("failed to read down log: %v", err)
	}
	if strings.TrimSpace(string(content)) != "database:abc123" {
		t.Fatalf("expected down to receive exports, got %q", content)
	}
}

func TestTeardownAllWaitsForActiveStart(t *testing.T) {
	tmpDir := setupResourceTestWorkspace(t)
	launchedFile := filepath.Join(tmpDir, "launched.marker")
	runningFile := filepath.Join(tmpDir, "running.marker")

	resource := &model.Resource{
		Label: label.TargetLabel{Package: "pkg", Name: "db"},
		Up:    "touch " + launchedFile + "; sleep 0.2; touch " + runningFile,
		Down:  "rm -f " + runningFile,
	}
	target := &model.Target{
		Label:        label.TargetLabel{Package: "pkg", Name: "consumer"},
		Dependencies: []label.TargetLabel{resource.Label},
	}
	graph := newResourceTestGraph(t, resource, target)
	if err := graph.AddEdge(resource, target); err != nil {
		t.Fatalf("failed to add edge: %v", err)
	}

	manager := NewResourceManager()
	startResult := make(chan error, 1)
	go func() {
		_, err := manager.EnsureResourcesStarted(context.Background(), graph, target, noopStatus)
		startResult <- err
	}()

	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, err := os.Stat(launchedFile); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("resource start did not launch")
		}
		time.Sleep(time.Millisecond)
	}

	manager.TeardownAll(context.Background())
	if err := <-startResult; err != nil {
		t.Fatalf("resource start failed: %v", err)
	}
	if _, err := os.Stat(runningFile); !os.IsNotExist(err) {
		t.Fatalf("resource remained after teardown: %v", err)
	}
}

func TestEnsureResourcesStartedResolvesResourceAlias(t *testing.T) {
	tmpDir := setupResourceTestWorkspace(t)
	startedFile := filepath.Join(tmpDir, "started.marker")

	resource := &model.Resource{
		Label:   label.TargetLabel{Package: "pkg", Name: "db"},
		Up:      "touch " + startedFile,
		Exports: map[string]string{"DATABASE_URL": "postgres://local"},
	}
	resourceAlias := &model.Alias{
		Label:  label.TargetLabel{Package: "pkg", Name: "database"},
		Actual: resource.Label,
	}
	target := &model.Target{
		Label:        label.TargetLabel{Package: "pkg", Name: "consumer"},
		Dependencies: []label.TargetLabel{resourceAlias.Label},
	}
	graph := newResourceTestGraph(t, resource, resourceAlias, target)
	if err := graph.AddEdge(resource, resourceAlias); err != nil {
		t.Fatalf("failed to add resource edge: %v", err)
	}
	if err := graph.AddEdge(resourceAlias, target); err != nil {
		t.Fatalf("failed to add alias edge: %v", err)
	}

	manager := NewResourceManager()
	environment, err := manager.EnsureResourcesStarted(context.Background(), graph, target, noopStatus)
	if err != nil {
		t.Fatalf("failed to start aliased resource: %v", err)
	}
	if _, err := os.Stat(startedFile); err != nil {
		t.Fatalf("aliased resource did not start: %v", err)
	}
	if !strings.Contains(strings.Join(environment, "\n"), "DATABASE_URL=postgres://local") {
		t.Fatalf("aliased resource exports missing: %v", environment)
	}
}

// TestEnsureResourcesStartedIsAtMostOnceAcrossSequentialConsumers guards
// against relying on singleflight as a cache: it only collapses calls that
// overlap in time, so a consumer arriving after the first start finished must
// still not re-run up.
func TestEnsureResourcesStartedIsAtMostOnceAcrossSequentialConsumers(t *testing.T) {
	tmpDir := setupResourceTestWorkspace(t)
	countFile := filepath.Join(tmpDir, "starts.log")

	resource := &model.Resource{
		Label: label.TargetLabel{Package: "pkg", Name: "db"},
		Up:    "echo up >> " + countFile,
	}
	firstConsumer := &model.Target{
		Label:        label.TargetLabel{Package: "pkg", Name: "first"},
		Dependencies: []label.TargetLabel{resource.Label},
	}
	secondConsumer := &model.Target{
		Label:        label.TargetLabel{Package: "pkg", Name: "second"},
		Dependencies: []label.TargetLabel{resource.Label},
	}
	graph := newResourceTestGraph(t, resource, firstConsumer, secondConsumer)
	for _, consumer := range []*model.Target{firstConsumer, secondConsumer} {
		if err := graph.AddEdge(resource, consumer); err != nil {
			t.Fatalf("failed to add edge: %v", err)
		}
	}

	manager := NewResourceManager()
	for _, consumer := range []*model.Target{firstConsumer, secondConsumer} {
		if _, err := manager.EnsureResourcesStarted(context.Background(), graph, consumer, noopStatus); err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
	}

	content, err := os.ReadFile(countFile)
	if err != nil {
		t.Fatalf("failed to read start counter: %v", err)
	}
	if got := strings.Count(string(content), "up"); got != 1 {
		t.Fatalf("expected exactly one up invocation across sequential consumers, got %d", got)
	}
}

// TestEnsureResourcesStartedStartsResourceDependencies verifies that a
// resource depending on another resource brings its dependency up first, can
// read its exports, and is torn down before it.
func TestEnsureResourcesStartedStartsResourceDependencies(t *testing.T) {
	tmpDir := setupResourceTestWorkspace(t)
	orderFile := filepath.Join(tmpDir, "order.log")

	database := &model.Resource{
		Label:   label.TargetLabel{Package: "pkg", Name: "database"},
		Up:      "echo database_up >> " + orderFile,
		Down:    "echo database_down >> " + orderFile,
		Exports: map[string]string{"DB_DSN": "postgres://local"},
	}
	migrations := &model.Resource{
		Label:        label.TargetLabel{Package: "pkg", Name: "migrations"},
		Up:           `echo "migrations_up:$DB_DSN" >> ` + orderFile,
		Down:         `echo "migrations_down:$DB_DSN" >> ` + orderFile,
		Dependencies: []label.TargetLabel{database.Label},
	}
	target := &model.Target{
		Label:        label.TargetLabel{Package: "pkg", Name: "consumer"},
		Dependencies: []label.TargetLabel{migrations.Label},
	}
	graph := newResourceTestGraph(t, database, migrations, target)
	if err := graph.AddEdge(database, migrations); err != nil {
		t.Fatalf("failed to add edge: %v", err)
	}
	if err := graph.AddEdge(migrations, target); err != nil {
		t.Fatalf("failed to add edge: %v", err)
	}

	manager := NewResourceManager()
	targetExports, err := manager.EnsureResourcesStarted(context.Background(), graph, target, noopStatus)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	manager.TeardownAll(context.Background())

	content, err := os.ReadFile(orderFile)
	if err != nil {
		t.Fatalf("failed to read order log: %v", err)
	}
	lines := strings.Fields(string(content))
	want := []string{
		"database_up",
		"migrations_up:postgres://local",
		"migrations_down:postgres://local",
		"database_down",
	}
	if len(lines) != len(want) {
		t.Fatalf("expected %v, got %v", want, lines)
	}
	for i, wantLine := range want {
		if lines[i] != wantLine {
			t.Fatalf("expected %v, got %v", want, lines)
		}
	}

	// The target only declared :migrations, so it must not inherit the
	// database's exports.
	if strings.Contains(strings.Join(targetExports, "\n"), "DB_DSN") {
		t.Errorf("expected target not to inherit transitive resource exports, got %v", targetExports)
	}
}

// TestTeardownAfterReadyTimeoutHasExports verifies that a resource whose up
// succeeded but whose ready probe timed out still tears down with its dynamic
// exports available, so cleanup can address the started instance.
func TestTeardownAfterReadyTimeoutHasExports(t *testing.T) {
	tmpDir := setupResourceTestWorkspace(t)
	downFile := filepath.Join(tmpDir, "down.log")

	resource := &model.Resource{
		Label:   label.TargetLabel{Package: "pkg", Name: "db"},
		Up:      `echo "CONTAINER_ID=abc123" >> "$GROG_RESOURCE_EXPORTS_FILE"`,
		Ready:   "false",
		Down:    `echo "removed:$CONTAINER_ID" >> ` + downFile,
		Timeout: 300 * time.Millisecond,
	}
	target := &model.Target{
		Label:        label.TargetLabel{Package: "pkg", Name: "consumer"},
		Dependencies: []label.TargetLabel{resource.Label},
	}
	graph := newResourceTestGraph(t, resource, target)
	if err := graph.AddEdge(resource, target); err != nil {
		t.Fatalf("failed to add edge: %v", err)
	}

	manager := NewResourceManager()
	if _, err := manager.EnsureResourcesStarted(context.Background(), graph, target, noopStatus); err == nil {
		t.Fatal("expected ready timeout error")
	}

	manager.TeardownAll(context.Background())

	content, err := os.ReadFile(downFile)
	if err != nil {
		t.Fatalf("failed to read down log: %v", err)
	}
	if strings.TrimSpace(string(content)) != "removed:abc123" {
		t.Fatalf("expected down to see the dynamic export, got %q", string(content))
	}
}
