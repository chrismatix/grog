package analysis

import (
	"testing"

	"grog/internal/dag"
	"grog/internal/label"
	"grog/internal/model"
)

func TestPathWithin(t *testing.T) {
	if !pathWithin("a", "a") {
		t.Fatal("same")
	}
	if !pathWithin("a/b", "a") {
		t.Fatal("child")
	}
	if pathWithin("a", "a/b") {
		t.Fatal("parent shouldn't be within")
	}
	if pathWithin("ab", "a") {
		t.Fatal("not a child despite prefix")
	}
}

func TestPathsOverlap(t *testing.T) {
	if !pathsOverlap("a/b", "a") {
		t.Fatal("nested")
	}
	if !pathsOverlap("a", "a/b") {
		t.Fatal("nested reversed")
	}
	if pathsOverlap("a", "b") {
		t.Fatal("unrelated")
	}
}

func TestBuildGraph_MissingDependency(t *testing.T) {
	t1 := &model.Target{
		Label:        label.TL("pkg", "a"),
		Dependencies: []label.TargetLabel{label.TL("pkg", "missing")},
	}
	nodes := model.BuildNodeMapFromNodes(t1)
	if _, err := BuildGraph(nodes); err == nil {
		t.Fatal("expected err for missing dep")
	}
}

func TestBuildGraph_Cycle(t *testing.T) {
	a := &model.Target{Label: label.TL("pkg", "a"), Dependencies: []label.TargetLabel{label.TL("pkg", "b")}}
	b := &model.Target{Label: label.TL("pkg", "b"), Dependencies: []label.TargetLabel{label.TL("pkg", "a")}}
	nodes := model.BuildNodeMapFromNodes(a, b)
	if _, err := BuildGraph(nodes); err == nil {
		t.Fatal("expected cycle err")
	}
}

func TestTargetsAreOrdered_NilCache(t *testing.T) {
	a := &model.Target{Label: label.TL("pkg", "a")}
	b := &model.Target{Label: label.TL("pkg", "b"), Dependencies: []label.TargetLabel{a.Label}}
	g := dag.NewDirectedGraphFromTargets(a, b)
	if err := g.AddEdge(a, b); err != nil {
		t.Fatal(err)
	}
	if !targetsAreOrdered(g, a, b, nil) {
		t.Fatal("expected ordered (b depends on a)")
	}
	if !targetsAreOrdered(g, b, a, nil) {
		t.Fatal("expected ordered reversed")
	}
}

func TestTargetsAreOrdered_Unordered(t *testing.T) {
	a := &model.Target{Label: label.TL("pkg", "a")}
	b := &model.Target{Label: label.TL("pkg", "b")}
	g := dag.NewDirectedGraphFromTargets(a, b)
	if targetsAreOrdered(g, a, b, nil) {
		t.Fatal("not ordered")
	}
}
