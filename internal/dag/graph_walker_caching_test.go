package dag

import (
	"context"
	"grog/internal/model"
	"testing"
)

func TestWalkerAllDepsCached(t *testing.T) {
	// Create a graph with linear dependencies: target1 -> target2 -> target3
	target1 := GetTarget("target1")
	target2 := GetTarget("target2")
	target3 := GetTarget("target3")

	graph := NewDirectedGraphFromTargets(target1, target2, target3)

	// target1 depends on target2, target2 depends on target3
	_ = graph.AddEdge(target2, target1)
	_ = graph.AddEdge(target3, target2)

	depsCachedMap := make(map[string]bool)

	walkFunc := func(ctx context.Context, target *model.Target, depsCached bool) (CacheResult, error) {
		// Record the depsCached value for this target
		depsCachedMap[target.Label.Name] = depsCached

		// Always return true to simulate cache hit for all targets
		return CacheHit, nil
	}

	walker := NewWalker(graph, walkFunc, false)
	ctx := context.Background()
	_, err := walker.Walk(ctx)

	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	// Verify depsCached was true for target2 (after target3 was cached)
	if !depsCachedMap["target2"] {
		t.Errorf("Expected depsCached to be true for target2, got false")
	}

	// Verify depsCached was true for target1 (after target2 was cached)
	if !depsCachedMap["target1"] {
		t.Errorf("Expected depsCached to be true for target1, got false")
	}

	// For target3 (leaf node with no dependencies), depsCached should be true by default
	if !depsCachedMap["target3"] {
		t.Errorf("Expected depsCached to be true for target3 (no deps), got false")
	}
}

func TestWalkerOneDepsNotCached(t *testing.T) {
	// Create a graph with linear dependencies: target1 -> target2 -> target3
	target1 := GetTarget("target1")
	target2 := GetTarget("target2")
	target3 := GetTarget("target3")

	graph := NewDirectedGraphFromTargets(target1, target2, target3)

	// target1 depends on target2, target2 depends on target3
	_ = graph.AddEdge(target2, target1)
	_ = graph.AddEdge(target3, target2)

	depsCachedMap := make(map[string]bool)

	walkFunc := func(ctx context.Context, target *model.Target, depsCached bool) (CacheResult, error) {
		depsCachedMap[target.Label.Name] = depsCached

		if target.Label.Name != "target3" {
			return CacheHit, nil
		}
		// Return CacheMiss only for target3 simulating a cache miss
		return CacheMiss, nil
	}

	walker := NewWalker(graph, walkFunc, false)
	ctx := context.Background()
	_, err := walker.Walk(ctx)

	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	// For target3 (leaf node with no dependencies), depsCached should be true by default
	if !depsCachedMap["target3"] {
		t.Errorf("Expected depsCached to be true for target3 (no deps), got false")
	}

	// Verify depsCached was false for target2 (since target3 was not cached)
	if depsCachedMap["target2"] {
		t.Errorf("Expected depsCached to be false for target2, got true")
	}

	// Verify depsCached was false for target1 (since target2 was not cached)
	if depsCachedMap["target1"] {
		t.Errorf("Expected depsCached to be false for target1, got true")
	}
}
