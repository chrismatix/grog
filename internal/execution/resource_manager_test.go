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
