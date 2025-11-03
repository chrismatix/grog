package dag

import (
	"grog/internal/label"
	"grog/internal/model"
	"reflect"
	"testing"
)

func TestDirectedTargetGraph_AddNode(t *testing.T) {
	graph := NewDirectedGraph()
	target1 := &model.Target{Label: label.TargetLabel{Name: "target1"}}
	target2 := &model.Target{Label: label.TargetLabel{Name: "target2"}}

	graph.AddNode(target1)
	// add idempotently
	graph.AddNode(target1)

	graph.AddNode(target2)

	if len(graph.nodes) != 2 {
		t.Errorf("Expected 2 nodes, got %d", len(graph.nodes))
	}
}

func TestDirectedTargetGraph_AddEdge(t *testing.T) {
	graph := NewDirectedGraph()
	target1 := &model.Target{Label: label.TargetLabel{Name: "target1"}}
	target2 := &model.Target{Label: label.TargetLabel{Name: "target2"}}
	target3 := &model.Target{Label: label.TargetLabel{Name: "target3"}}

	graph.AddNode(target1)
	graph.AddNode(target2)

	err := graph.AddEdge(target1, target2)
	if err != nil {
		t.Fatalf("AddEdge failed: %v", err)
	}

	err = graph.AddEdge(target1, target2) // Adding the same edge again should be fine, as we use append
	if err != nil {
		t.Fatalf("AddEdge failed: %v", err)
	}

	err = graph.AddEdge(target1, target1)
	if err == nil {
		t.Errorf("AddEdge should have returned an error for self-loop")
	}

	err = graph.AddEdge(target1, target3)
	if err == nil {
		t.Errorf("AddEdge should have returned an error for non-existent 'to' node")
	}

	err = graph.AddEdge(target3, target1)
	if err == nil {
		t.Errorf("AddEdge should have returned an error for non-existent 'from' node")
	}

	if len(graph.outEdges[target1.Label]) != 2 {
		t.Errorf("Expected 2 outEdges from target1, got %d", len(graph.outEdges[target1.Label]))
	}

	if len(graph.inEdges[target2.Label]) != 2 {
		t.Errorf("Expected 2 inEdges to target2, got %d", len(graph.inEdges[target2.Label]))
	}
}

func TestDirectedTargetGraph_GetInEdges(t *testing.T) {
	graph := NewDirectedGraph()
	target1 := &model.Target{Label: label.TargetLabel{Name: "target1"}}
	target2 := &model.Target{Label: label.TargetLabel{Name: "target2"}}
	target3 := &model.Target{Label: label.TargetLabel{Name: "target3"}}

	graph.AddNode(target1)
	graph.AddNode(target2)
	graph.AddNode(target3)

	graph.AddEdge(target1, target2)
	graph.AddEdge(target3, target2)

	inEdges := graph.GetDependencies(target2)

	expectedInEdges := []model.BuildNode{target1, target3}
	if !reflect.DeepEqual(inEdges, expectedInEdges) {
		t.Errorf("GetDependencies returned incorrect inEdges. Expected %v, got %v", expectedInEdges, inEdges)
	}
}

func TestDirectedTargetGraph_GetOutEdges(t *testing.T) {
	graph := NewDirectedGraph()
	target1 := &model.Target{Label: label.TargetLabel{Name: "target1"}}
	target2 := &model.Target{Label: label.TargetLabel{Name: "target2"}}
	target3 := &model.Target{Label: label.TargetLabel{Name: "target3"}}

	graph.AddNode(target1)
	graph.AddNode(target2)
	graph.AddNode(target3)

	graph.AddEdge(target2, target1)
	graph.AddEdge(target2, target3)

	outEdges := graph.GetDependants(target2)

	expectedOutEdges := []model.BuildNode{target1, target3} // Two edges added in AddEdge test
	if !reflect.DeepEqual(outEdges, expectedOutEdges) {
		t.Errorf("GetDependants returned incorrect outEdges. Expected %v, got %v", expectedOutEdges, outEdges)
	}
}

func TestDirectedTargetGraph_hasNode(t *testing.T) {
	graph := NewDirectedGraph()
	target1 := &model.Target{Label: label.TargetLabel{Name: "target1"}}
	target2 := &model.Target{Label: label.TargetLabel{Name: "target2"}}

	graph.AddNode(target1)

	if !graph.hasNode(target1) {
		t.Errorf("hasNode should have returned true for existing node")
	}

	if graph.hasNode(target2) {
		t.Errorf("hasNode should have returned false for non-existent node")
	}
}

func TestDirectedTargetGraph_HasCycle(t *testing.T) {
	graph := NewDirectedGraph()
	target1 := &model.Target{Label: label.TargetLabel{Name: "target1"}}
	target2 := &model.Target{Label: label.TargetLabel{Name: "target2"}}
	target3 := &model.Target{Label: label.TargetLabel{Name: "target3"}}

	graph.AddNode(target1)
	graph.AddNode(target2)
	graph.AddNode(target3)

	// No cycle
	if graph.HasCycle() {
		t.Errorf("HasCycle should have returned false for an empty graph")
	}

	// Add edges to create a cycle: target1 -> target2 -> target3 -> target1
	graph.AddEdge(target1, target2)
	graph.AddEdge(target2, target3)
	graph.AddEdge(target3, target1)

	if !graph.HasCycle() {
		t.Errorf("HasCycle should have returned true for a graph with a cycle")
	}

	// Remove one edge to break the cycle: target1 -> target2 -> target3
	graph.outEdges[target3.Label] = []model.BuildNode{}

	// Check if there is a cycle
	graph = NewDirectedGraph()
	target1 = &model.Target{Label: label.TargetLabel{Name: "target1"}}
	target2 = &model.Target{Label: label.TargetLabel{Name: "target2"}}
	target3 = &model.Target{Label: label.TargetLabel{Name: "target3"}}

	graph.AddNode(target1)
	graph.AddNode(target2)
	graph.AddNode(target3)

	graph.AddEdge(target1, target2)
	graph.AddEdge(target2, target3)

	if graph.HasCycle() {
		t.Errorf("HasCycle should have returned false for a graph without a cycle")
	}
}

func TestDirectedTargetGraph_FindCycle(t *testing.T) {
	graph := NewDirectedGraph()
	target1 := &model.Target{Label: label.TargetLabel{Name: "target1"}}
	target2 := &model.Target{Label: label.TargetLabel{Name: "target2"}}
	target3 := &model.Target{Label: label.TargetLabel{Name: "target3"}}

	graph.AddNode(target1)
	graph.AddNode(target2)
	graph.AddNode(target3)

	graph.AddEdge(target1, target2)
	graph.AddEdge(target2, target3)
	graph.AddEdge(target3, target1)

	cycle, found := graph.FindCycle()
	if !found {
		t.Fatalf("expected to find a cycle")
	}

	expected := []model.BuildNode{target1, target2, target3, target1}
	if len(cycle) != len(expected) {
		t.Fatalf("unexpected cycle length: got %d, want %d", len(cycle), len(expected))
	}

	for i := range expected {
		if cycle[i] != expected[i] {
			t.Fatalf("unexpected cycle node at %d: got %s, want %s", i, cycle[i].GetLabel(), expected[i].GetLabel())
		}
	}
}

func TestDirectedTargetGraph_GetDescendants(t *testing.T) {
	graph := NewDirectedGraph()

	// Create test targets
	target1 := &model.Target{Label: label.TargetLabel{Name: "target1"}}
	target2 := &model.Target{Label: label.TargetLabel{Name: "target2"}}
	target3 := &model.Target{Label: label.TargetLabel{Name: "target3"}}
	target4 := &model.Target{Label: label.TargetLabel{Name: "target4"}}
	target5 := &model.Target{Label: label.TargetLabel{Name: "target5"}}

	// Add nodes
	graph.AddNode(target1)
	graph.AddNode(target2)
	graph.AddNode(target3)
	graph.AddNode(target4)
	graph.AddNode(target5)

	// Create a graph structure:
	// target1 -> target2 -> target4
	//       \-> target3 -> target5
	graph.AddEdge(target1, target2)
	graph.AddEdge(target1, target3)
	graph.AddEdge(target2, target4)
	graph.AddEdge(target3, target5)

	// Test case 1: target1 should have all other targets as descendants
	descendants1 := graph.GetDescendants(target1)
	expectedDescendants1 := []*model.Target{target2, target4, target3, target5}

	// We need to check that all expected descendants are in the actual descendants list
	// Order might vary, so we'll use a map to check for presence
	if len(descendants1) != len(expectedDescendants1) {
		t.Errorf("GetDescendants for target1 returned wrong number of descendants. Expected %descendant, got %descendant",
			len(expectedDescendants1), len(descendants1))
	}

	descendantMap := make(map[string]bool)
	for _, descendant := range descendants1 {
		descendantMap[descendant.GetLabel().Name] = true
	}

	for _, expected := range expectedDescendants1 {
		if !descendantMap[expected.Label.Name] {
			t.Errorf("Expected descendant %s not found in result", expected.Label.Name)
		}
	}

	// Test case 2: target2 should have only target4 as descendant
	descendants2 := graph.GetDescendants(target2)
	if len(descendants2) != 1 || descendants2[0] != target4 {
		t.Errorf("GetDescendants for target2 returned incorrect result. Expected [target4], got %v", descendants2)
	}

	// Test case 3: target4 should have no descendants
	descendants4 := graph.GetDescendants(target4)
	if len(descendants4) != 0 {
		t.Errorf("GetDescendants for target4 should return empty slice, got %v", descendants4)
	}
}
