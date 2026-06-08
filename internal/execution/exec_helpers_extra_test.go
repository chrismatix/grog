package execution

import (
	"testing"

	"grog/internal/config"
	"grog/internal/dag"
	"grog/internal/label"
	"grog/internal/model"
	"grog/internal/output/handlers"
)

func TestGetDependencyOutputIdentifiersSkipsDepsWithoutOutputs(t *testing.T) {
	prev := config.Global
	config.Global = config.WorkspaceConfig{WorkspaceRoot: "/ws", Root: "/ws"}
	t.Cleanup(func() { config.Global = prev })

	dep := &model.Target{
		Label: label.TargetLabel{Package: "p", Name: "dep"},
	}
	consumer := &model.Target{
		Label:        label.TargetLabel{Package: "p", Name: "c"},
		Dependencies: []label.TargetLabel{dep.Label},
	}
	graph := dag.NewDirectedGraphFromTargets(consumer, dep)
	graph.AddEdge(dep, consumer)

	executor := &Executor{graph: graph}
	got := executor.getDependencyOutputIdentifiers(consumer)
	if len(got) != 0 {
		t.Fatalf("expected empty map (dep has no outputs), got %v", got)
	}
}

func TestGetDependencyOutputIdentifiersHonorsShortenedLabels(t *testing.T) {
	prev := config.Global
	config.Global = config.WorkspaceConfig{WorkspaceRoot: "/ws", Root: "/ws"}
	t.Cleanup(func() { config.Global = prev })

	dep := &model.Target{
		Label:   label.TargetLabel{Package: "lib/foo", Name: "foo"},
		Outputs: []model.Output{model.NewOutput(string(handlers.FileHandler), "f.txt")},
	}
	consumer := &model.Target{
		Label:        label.TargetLabel{Package: "other", Name: "c"},
		Dependencies: []label.TargetLabel{dep.Label},
	}
	graph := dag.NewDirectedGraphFromTargets(consumer, dep)
	graph.AddEdge(dep, consumer)

	executor := &Executor{graph: graph}
	got := executor.getDependencyOutputIdentifiers(consumer)
	if _, ok := got["//lib/foo"]; !ok {
		t.Fatalf("expected //lib/foo shortened key, got %v", got)
	}
}

func TestGetTransitiveOutputsByTagSkipsNonTargetAncestors(t *testing.T) {
	prev := config.Global
	config.Global = config.WorkspaceConfig{WorkspaceRoot: "/ws", Root: "/ws"}
	t.Cleanup(func() { config.Global = prev })

	root := &model.Target{Label: label.TargetLabel{Package: "p", Name: "r"}}
	graph := dag.NewDirectedGraphFromTargets(root)
	executor := &Executor{graph: graph}
	if got := executor.getTransitiveOutputsByTag(root); len(got) != 0 {
		t.Fatalf("expected empty map, got %v", got)
	}
}
