package dag

import (
	"context"
	"errors"
	"grog/internal/label"
	"grog/internal/model"
	"sync"
	"testing"
)

func GetTarget(name string) *model.Target {
	return &model.Target{Label: label.TargetLabel{Name: name}, IsSelected: true}
}

func TestWalkerBasic(t *testing.T) {
	// Create a simple graph with no dependencies
	target1 := GetTarget("target1")
	target2 := GetTarget("target2")

	graph := NewDirectedGraphFromTargets(target1, target2)

	graph.AddVertex(target1)
	graph.AddVertex(target2)

	// Track execution order
	var executionOrder []label.TargetLabel
	var mu sync.Mutex

	walkFunc := func(ctx context.Context, target *model.Target, depsCached bool) (bool, error) {
		mu.Lock()
		defer mu.Unlock()
		executionOrder = append(executionOrder, target.Label)
		return false, nil
	}

	walker := NewWalker(graph, walkFunc, true)

	ctx := context.Background()
	completionMap, err := walker.Walk(ctx)

	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	// Since there are no dependencies, both targets should be executed
	if len(executionOrder) != 2 {
		t.Errorf("Expected 2 targets to be executed, got %d", len(executionOrder))
	}

	// Verify that all targets were completed successfully
	if len(completionMap) != 2 {
		t.Errorf("Expected 2 targets in completion map, got %d", len(completionMap))
	}

	for _, target := range []*model.Target{target1, target2} {
		completion, ok := completionMap[target]
		if !ok {
			t.Errorf("Expected target %s to be in completion map", target.Label)
		}
		if !completion.IsSuccess {
			t.Errorf("Expected target %s to be successful", target.Label)
		}
		if completion.Err != nil {
			t.Errorf("Expected target %s to have no error, got %v", target.Label, completion.Err)
		}
	}
}

func TestWalkerLinearDependency(t *testing.T) {
	// Create a graph with linear dependencies: target1 -> target2 -> target3
	target1 := GetTarget("target1")
	target2 := GetTarget("target2")
	target3 := GetTarget("target3")

	graph := NewDirectedGraphFromTargets(
		target1,
		target2,
		target3)

	// target1 depends on target2, target2 depends on target3
	_ = graph.AddEdge(target2, target1) // target1 has target2 as dependency
	_ = graph.AddEdge(target3, target2) // target2 has target3 as dependency

	// Track execution order
	var executionOrder []label.TargetLabel
	var mu sync.Mutex

	walkFunc := func(ctx context.Context, target *model.Target, depsCached bool) (bool, error) {
		mu.Lock()
		defer mu.Unlock()
		executionOrder = append(executionOrder, target.Label)
		return false, nil
	}

	walker := NewWalker(graph, walkFunc, true)

	ctx := context.Background()
	completionMap, err := walker.Walk(ctx)

	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	// Verify that target3 was processed before target2, and target2 before target1
	found1, found2, found3 := -1, -1, -1
	for i, tl := range executionOrder {
		switch tl.Name {
		case "target1":
			found1 = i
		case "target2":
			found2 = i
		case "target3":
			found3 = i
		}
	}

	if found3 > found2 || found2 > found1 {
		t.Errorf("Invalid execution order: %v", executionOrder)
	}

	// Verify that all targets were completed successfully
	if len(completionMap) != 3 {
		t.Errorf("Expected 3 targets in completion map, got %d", len(completionMap))
	}

	for _, target := range []*model.Target{target1, target2, target3} {
		completion, ok := completionMap[target]
		if !ok {
			t.Errorf("Expected target %s to be in completion map", target.Label)
		}
		if !completion.IsSuccess {
			t.Errorf("Expected target %s to be successful", target.Label)
		}
		if completion.Err != nil {
			t.Errorf("Expected target %s to have no error, got %v", target.Label, completion.Err)
		}
	}
}

func TestWalkerDiamondDependency(t *testing.T) {
	// Create a diamond dependency graph:
	//          target1
	//         /      \
	//    target2     target3
	//         \      /
	//          target4
	target1 := GetTarget("target1")
	target2 := GetTarget("target2")
	target3 := GetTarget("target3")
	target4 := GetTarget("target4")

	graph := NewDirectedGraphFromTargets(
		target1,
		target2,
		target3,
		target4)

	_ = graph.AddEdge(target2, target1)
	_ = graph.AddEdge(target3, target1)
	_ = graph.AddEdge(target4, target2)
	_ = graph.AddEdge(target4, target3)

	// Track execution
	var executedTargets []label.TargetLabel
	var mu sync.Mutex

	walkFunc := func(ctx context.Context, target *model.Target, depsCached bool) (bool, error) {
		mu.Lock()
		defer mu.Unlock()
		executedTargets = append(executedTargets, target.Label)
		return false, nil
	}

	walker := NewWalker(graph, walkFunc, true)

	ctx := context.Background()
	completionMap, err := walker.Walk(ctx)

	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	// All targets should be executed
	if len(executedTargets) != 4 {
		t.Errorf("Expected 4 targets to be executed, got %d", len(executedTargets))
	}

	// Find positions
	pos := make(map[string]int)
	for i, tl := range executedTargets {
		pos[tl.String()] = i
	}

	// Verify target4 is processed before target2 and target3
	// And target2 and target3 are processed before target1
	if pos["target4"] > pos["target2"] || pos["target4"] > pos["target3"] {
		t.Errorf("target4 should be processed before target2 and target3")
	}

	if pos["target2"] > pos["target1"] || pos["target3"] > pos["target1"] {
		t.Errorf("target2 and target3 should be processed before target1")
	}

	// Verify that all targets were completed successfully
	if len(completionMap) != 4 {
		t.Errorf("Expected 4 targets in completion map, got %d", len(completionMap))
	}

	for _, target := range []*model.Target{target1, target2, target3, target4} {
		completion, ok := completionMap[target]
		if !ok {
			t.Errorf("Expected target %s to be in completion map", target.Label)
		}
		if !completion.IsSuccess {
			t.Errorf("Expected target %s to be successful", target.Label)
		}
		if completion.Err != nil {
			t.Errorf("Expected target %s to have no error, got %v", target.Label, completion.Err)
		}
	}
}

func TestWalkerFailFast(t *testing.T) {
	target1 := GetTarget("target1")
	target2 := GetTarget("target2")
	target3 := GetTarget("target3")

	// Create a graph with a dependency that will fail
	graph := NewDirectedGraphFromTargets(target1, target2, target3)

	// target1 depends on target2
	// target3 is independent
	_ = graph.AddEdge(target2, target1)
	target2Chan := make(chan bool)

	// walkFunc that fails for target2
	walkFunc := func(ctx context.Context, target *model.Target, depsCached bool) (bool, error) {
		if target.Label.Name == "target2" {
			go func() {
				target2Chan <- true
			}()
			return false, errors.New("failed to execute target2")
		}
		if target.Label.Name == "target3" {
			// wait for target2 to complete to simulate a task that would take longer than target2
			<-target2Chan
			return false, nil
		}
		return false, nil
	}

	walker := NewWalker(graph, walkFunc, true) // failFast = true

	ctx := context.Background()
	completionMap, err := walker.Walk(ctx)

	if err != nil {
		t.Fatalf("Returned error should not be nil, got %v", err)
	}

	// target2 should have failed
	completion2, ok := completionMap[target2]
	if !ok {
		t.Errorf("Expected target2 to be in completion map")
	}
	if completion2.IsSuccess {
		t.Errorf("Expected target2 to have failed")
	}
	if completion2.Err == nil {
		t.Errorf("Expected target2 to have an error")
	}

	// target1 might or might not have started, but should not be successful
	_, ok = completionMap[target1]
	if ok {
		t.Errorf("target1 should not have been processed")
	}

	// target3 should not have completed
	completion, ok := completionMap[target3]
	if ok && completion.IsSuccess {
		t.Errorf("target3 should not have completed successfully")
	}
}

func TestWalkerNonFailFast(t *testing.T) {
	// Create a graph with a dependency that will fail
	target1 := GetTarget("target1")
	target2 := GetTarget("target2")
	target3 := GetTarget("target3")
	target4 := GetTarget("target4")

	graph := NewDirectedGraphFromTargets(target1, target2, target3, target4)

	graph.AddVertex(target1)
	graph.AddVertex(target2)
	graph.AddVertex(target3)
	graph.AddVertex(target4)

	_ = graph.AddEdge(target1, target2)
	// target3 and target4 are on a different branch
	_ = graph.AddEdge(target3, target4)

	// walkFunc that fails for target2
	walkFunc := func(ctx context.Context, target *model.Target, depsCached bool) (bool, error) {
		if target.Label.Name == "target1" {
			return false, errors.New("failed to execute target1")
		}
		return false, nil
	}

	walker := NewWalker(graph, walkFunc, false) // failFast = false

	ctx := context.Background()
	completionMap, err := walker.Walk(ctx)

	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	// target1 should have failed
	completion1, ok := completionMap[target1]
	if !ok {
		t.Errorf("Expected target1 to be in completion map")
	}
	if completion1.IsSuccess {
		t.Errorf("Expected target1 to have failed")
	}
	if completion1.Err == nil {
		t.Errorf("Expected target1 to have an error")
	}

	// target2 should not be in completion map
	_, ok = completionMap[target2]
	if ok {
		t.Errorf("Did not expect target2 to be in completion map")
	}

	if len(completionMap) != 3 {
		t.Errorf("Expected 3 targets in completion map, got %d", len(completionMap))
	}
}

func TestWalkerEdgeCases(t *testing.T) {
	t.Run("NilWalkFunction", func(t *testing.T) {
		// Test with a nil walk function
		graph := NewDirectedGraphFromTargets(GetTarget("A"))

		// Pass nil walk function
		walker := NewWalker(graph, nil, true)

		_, err := walker.Walk(context.Background())
		if err == nil {
			t.Errorf("Expected error, got nil")
		}
	})
}
