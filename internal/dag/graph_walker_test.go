package dag

import (
	"context"
	"errors"
	"fmt"
	"grog/internal/label"
	"grog/internal/model"
	"sync"
	"testing"
	"time"
)

func GetTarget(name string) *model.Target {
	return &model.Target{Label: label.TargetLabel{Name: name}}
}

func TestWalkerBasic(t *testing.T) {
	// Create a simple graph with no dependencies
	graph := NewDirectedGraph()

	target1 := GetTarget("target1")
	target2 := GetTarget("target2")

	graph.AddVertex(target1)
	graph.AddVertex(target2)

	// Track execution order
	var executionOrder []label.TargetLabel
	var mu sync.Mutex

	walkFunc := func(ctx context.Context, target model.Target) error {
		mu.Lock()
		defer mu.Unlock()
		executionOrder = append(executionOrder, target.Label)
		return nil
	}

	walker := NewWalker(graph, walkFunc, true)

	ctx := context.Background()
	err := walker.Walk(ctx)

	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	// Since there are no dependencies, both targets should be executed
	if len(executionOrder) != 2 {
		t.Errorf("Expected 2 targets to be executed, got %d", len(executionOrder))
	}
}

func TestWalkerLinearDependency(t *testing.T) {
	// Create a graph with linear dependencies: target1 -> target2 -> target3
	graph := NewDirectedGraph()

	target1 := GetTarget("target1")
	target2 := GetTarget("target2")
	target3 := GetTarget("target3")

	graph.AddVertex(target1)
	graph.AddVertex(target2)
	graph.AddVertex(target3)

	// target1 depends on target2, target2 depends on target3
	_ = graph.AddEdge(target2, target1) // target1 has target2 as dependency
	_ = graph.AddEdge(target3, target2) // target2 has target3 as dependency

	// Track execution order
	var executionOrder []label.TargetLabel
	var mu sync.Mutex

	walkFunc := func(ctx context.Context, target model.Target) error {
		mu.Lock()
		defer mu.Unlock()
		executionOrder = append(executionOrder, target.Label)
		return nil
	}

	walker := NewWalker(graph, walkFunc, true)

	ctx := context.Background()
	err := walker.Walk(ctx)

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
}

func TestWalkerDiamondDependency(t *testing.T) {
	// Create a diamond dependency graph:
	//          target1
	//         /      \
	//    target2     target3
	//         \      /
	//          target4
	graph := NewDirectedGraph()

	target1 := GetTarget("target1")
	target2 := GetTarget("target2")
	target3 := GetTarget("target3")
	target4 := GetTarget("target4")

	graph.AddVertex(target1)
	graph.AddVertex(target2)
	graph.AddVertex(target3)
	graph.AddVertex(target4)

	_ = graph.AddEdge(target2, target1)
	_ = graph.AddEdge(target3, target1)
	_ = graph.AddEdge(target4, target2)
	_ = graph.AddEdge(target4, target3)

	// Track execution
	var executedTargets []label.TargetLabel
	var mu sync.Mutex

	walkFunc := func(ctx context.Context, target model.Target) error {
		mu.Lock()
		defer mu.Unlock()
		executedTargets = append(executedTargets, target.Label)
		return nil
	}

	walker := NewWalker(graph, walkFunc, true)

	ctx := context.Background()
	err := walker.Walk(ctx)

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
}

func TestWalkerErrorHandling(t *testing.T) {
	// Test that errors are properly handled
	graph := NewDirectedGraph()

	target1 := GetTarget("target1")
	target2 := GetTarget("target2")
	target3 := GetTarget("target3")

	graph.AddVertex(target1)
	graph.AddVertex(target2)
	graph.AddVertex(target3)

	_ = graph.AddEdge(target2, target1) // target1 has target2 as dependency
	_ = graph.AddEdge(target3, target2) // target2 has target3 as dependency

	expectedError := errors.New("simulated error")

	// Track execution
	executed := make(map[string]bool)
	var mu sync.Mutex

	walkFunc := func(ctx context.Context, target model.Target) error {
		mu.Lock()
		defer mu.Unlock()
		executed[target.Label.Name] = true

		// Simulate target2 failing
		if target.Label.Name == "target2" {
			return expectedError
		}
		return nil
	}

	// Test with failFast = true
	t.Run("FailFast=true", func(t *testing.T) {
		executed = make(map[string]bool)
		walker := NewWalker(graph, walkFunc, true)

		ctx := context.Background()
		err := walker.Walk(ctx)

		if err != nil {
			t.Fatalf("Walker should not return error directly: %v", err)
		}

		// target3 should have executed, target2 should have failed
		if !executed["target3"] {
			t.Errorf("target3 should have been executed")
		}
		if !executed["target2"] {
			t.Errorf("target2 should have been executed")
		}
		// target1 should not have executed since target2 failed
		if executed["target1"] {
			t.Errorf("target1 should not have been executed after target2 failed")
		}
	})

	// Test with failFast = false
	t.Run("FailFast=false", func(t *testing.T) {
		executed = make(map[string]bool)
		walker := NewWalker(graph, walkFunc, false)

		ctx := context.Background()
		err := walker.Walk(ctx)

		if err != nil {
			t.Fatalf("Walker should not return error directly: %v", err)
		}

		// target3 should have executed, target2 should have failed
		if !executed["target3"] {
			t.Errorf("target3 should have been executed")
		}
		if !executed["target2"] {
			t.Errorf("target2 should have been executed")
		}
		// target1 should still not execute since it depends on target2
		if executed["target1"] {
			t.Errorf("target1 should not have been executed because its dependency failed")
		}
	})
}

func TestWalkerContextCancellation(t *testing.T) {
	graph := NewDirectedGraph()

	// Create many targets to ensure the test has time to cancel
	var targets []*model.Target
	for i := 0; i < 20; i++ {
		target := GetTarget(fmt.Sprintf("target%d", i))
		targets = append(targets, target)
		graph.AddVertex(target)
	}

	// Make each target depend on the previous one
	for i := 1; i < len(targets); i++ {
		_ = graph.AddEdge(targets[i-1], targets[i])
	}

	// Track execution
	var executed []string
	var mu sync.Mutex

	// Walkfunc with artificial delay
	walkFunc := func(ctx context.Context, target model.Target) error {
		mu.Lock()
		executed = append(executed, target.Label.Name)
		mu.Unlock()

		// Artificial delay
		time.Sleep(50 * time.Millisecond)

		return nil
	}

	walker := NewWalker(graph, walkFunc, true)

	// Create a context with cancellation
	ctx, cancel := context.WithCancel(context.Background())

	// Cancel after a short delay
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()

	err := walker.Walk(ctx)

	// Should receive context cancellation error
	if err != context.Canceled {
		t.Fatalf("Expected context.Canceled error, got %v", err)
	}

	// Not all targets should have been executed
	if len(executed) == len(targets) {
		t.Errorf("Expected not all targets to execute, but all %d did", len(targets))
	}
}

func TestWalkerComplexDependencies(t *testing.T) {
	// Create more complex dependency graph
	graph := NewDirectedGraph()

	targetA := GetTarget("A")
	targetB := GetTarget("B")
	targetC := GetTarget("C")
	targetD := GetTarget("D")
	targetE := GetTarget("E")
	targetF := GetTarget("F")

	graph.AddVertex(targetA)
	graph.AddVertex(targetB)
	graph.AddVertex(targetC)
	graph.AddVertex(targetD)
	graph.AddVertex(targetE)
	graph.AddVertex(targetF)

	// Dependencies:
	// A depends on B, C
	// B depends on D, E
	// C depends on E, F

	_ = graph.AddEdge(targetB, targetA)
	_ = graph.AddEdge(targetC, targetA)
	_ = graph.AddEdge(targetD, targetB)
	_ = graph.AddEdge(targetE, targetB)
	_ = graph.AddEdge(targetE, targetC)
	_ = graph.AddEdge(targetF, targetC)

	// Track execution order
	var executionOrder []string
	var mu sync.Mutex

	walkFunc := func(ctx context.Context, target model.Target) error {
		mu.Lock()
		defer mu.Unlock()
		executionOrder = append(executionOrder, target.Label.Name)
		return nil
	}

	walker := NewWalker(graph, walkFunc, true)

	ctx := context.Background()
	err := walker.Walk(ctx)

	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	// All targets should be executed
	if len(executionOrder) != 6 {
		t.Errorf("Expected 6 targets to be executed, got %d", len(executionOrder))
	}

	// Verify dependencies
	pos := make(map[string]int)
	for i, label := range executionOrder {
		pos[label] = i
	}

	// Verify that dependencies were executed before dependents
	if pos["A"] < pos["B"] || pos["A"] < pos["C"] {
		t.Errorf("A should be executed after B and C")
	}

	if pos["B"] < pos["D"] || pos["B"] < pos["E"] {
		t.Errorf("B should be executed after D and E")
	}

	if pos["C"] < pos["E"] || pos["C"] < pos["F"] {
		t.Errorf("C should be executed after E and F")
	}
}

func TestWalkerEmptyGraph(t *testing.T) {
	// Test with an empty graph
	graph := NewDirectedGraph()

	var executed bool
	walkFunc := func(ctx context.Context, target model.Target) error {
		executed = true
		return nil
	}

	walker := NewWalker(graph, walkFunc, true)

	ctx := context.Background()
	err := walker.Walk(ctx)

	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	if executed {
		t.Errorf("No targets should have been executed in an empty graph")
	}
}

func TestWalkerTargetWithNoDependencies(t *testing.T) {
	// Test that targets with no dependencies start immediately
	graph := NewDirectedGraph()

	targetA := GetTarget("A")
	targetB := GetTarget("B")
	targetC := GetTarget("C")

	graph.AddVertex(targetA)
	graph.AddVertex(targetB)
	graph.AddVertex(targetC)

	// B depends on A
	_ = graph.AddEdge(targetA, targetB)

	// C has no dependencies

	// Track execution times
	executionTimes := make(map[string]time.Time)
	var mu sync.Mutex

	walkFunc := func(ctx context.Context, target model.Target) error {
		mu.Lock()
		executionTimes[target.Label.Name] = time.Now()
		mu.Unlock()
		// Add delay to ensure clear timing differences
		// between targets that started immediately vs dependants
		time.Sleep(50 * time.Millisecond)
		return nil
	}

	walker := NewWalker(graph, walkFunc, true)

	ctx := context.Background()
	startTime := time.Now()
	err := walker.Walk(ctx)

	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	// A and C should start immediately, B should wait for A
	if len(executionTimes) != 3 {
		t.Fatalf("Expected 3 targets to be executed, got %d", len(executionTimes))
	}

	// A and C should start quickly
	if executionTimes["A"].Sub(startTime) > 10*time.Millisecond {
		t.Errorf("Target A should start immediately")
	}

	if executionTimes["C"].Sub(startTime) > 10*time.Millisecond {
		t.Errorf("Target C should start immediately")
	}

	// B should wait for A
	if executionTimes["B"].Sub(executionTimes["A"]) < 40*time.Millisecond {
		t.Errorf("Target B should wait for target A to finish")
	}
}

func TestWalkerCascadingErrors(t *testing.T) {
	// Test cascading errors: if A fails, B and C should not execute
	graph := NewDirectedGraph()

	targetA := GetTarget("A")
	targetB := GetTarget("B")
	targetC := GetTarget("C")
	targetD := GetTarget("D")

	graph.AddVertex(targetA)
	graph.AddVertex(targetB)
	graph.AddVertex(targetC)
	graph.AddVertex(targetD)

	// D depends on A, B, C
	// C depends on B
	// B depends on A
	_ = graph.AddEdge(targetA, targetB)
	_ = graph.AddEdge(targetB, targetC)
	_ = graph.AddEdge(targetA, targetD)
	_ = graph.AddEdge(targetB, targetD)
	_ = graph.AddEdge(targetC, targetD)

	// Track execution
	var executed []string
	var mu sync.Mutex

	walkFunc := func(ctx context.Context, target model.Target) error {
		mu.Lock()
		defer mu.Unlock()
		executed = append(executed, target.Label.Name)

		// A fails
		if target.Label.Name == "A" {
			return errors.New("simulated failure in A")
		}
		return nil
	}

	walker := NewWalker(graph, walkFunc, true)

	ctx := context.Background()
	err := walker.Walk(ctx)

	if err != nil {
		t.Fatalf("Walker should not return error directly: %v", err)
	}

	// Only A should have executed
	if len(executed) != 1 || executed[0] != "A" {
		t.Errorf("Expected only A to execute, got: %v", executed)
	}
}

func TestWalkerConcurrencyLimits(t *testing.T) {
	// Test that many independent targets can run concurrently
	graph := NewDirectedGraph()

	var targets []*model.Target
	for i := 0; i < 10; i++ {
		target := GetTarget(fmt.Sprintf("target%d", i))
		targets = append(targets, target)
		graph.AddVertex(target)
	}

	// Track execution concurrency
	var maxConcurrent int
	var currentConcurrent int
	var mu sync.Mutex

	walkFunc := func(ctx context.Context, target model.Target) error {
		mu.Lock()
		currentConcurrent++
		if currentConcurrent > maxConcurrent {
			maxConcurrent = currentConcurrent
		}
		mu.Unlock()

		// Simulate work
		time.Sleep(50 * time.Millisecond)

		mu.Lock()
		currentConcurrent--
		mu.Unlock()

		return nil
	}

	walker := NewWalker(graph, walkFunc, true)

	ctx := context.Background()
	err := walker.Walk(ctx)

	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	// With 10 independent targets, we should have seen multiple running concurrently
	if maxConcurrent < 2 {
		t.Errorf("Expected multiple targets to run concurrently, but max concurrent was %d", maxConcurrent)
	}
}

func TestWalkerEdgeCases(t *testing.T) {
	t.Run("NilWalkFunction", func(t *testing.T) {
		// Test with a nil walk function
		graph := NewDirectedGraph()

		targetA := GetTarget("A")
		graph.AddVertex(targetA)

		// Pass nil walk function
		walker := NewWalker(graph, nil, true)

		err := walker.Walk(context.Background())
		if err == nil {
			t.Errorf("Expected error, got nil")
		}
	})
}
