package dag

import (
	"grog/internal/label"
	"grog/internal/model"
	"reflect"
	"testing"
)

func TestDirectedTargetGraph_AddVertex(t *testing.T) {
	graph := NewDirectedGraph()
	target1 := &model.Target{Label: label.TargetLabel{Name: "target1"}}
	target2 := &model.Target{Label: label.TargetLabel{Name: "target2"}}

	graph.AddVertex(target1)
	// add idempotently
	graph.AddVertex(target1)

	graph.AddVertex(target2)

	if len(graph.vertices) != 2 {
		t.Errorf("Expected 2 vertices, got %d", len(graph.vertices))
	}
}

func TestDirectedTargetGraph_AddEdge(t *testing.T) {
	graph := NewDirectedGraph()
	target1 := &model.Target{Label: label.TargetLabel{Name: "target1"}}
	target2 := &model.Target{Label: label.TargetLabel{Name: "target2"}}
	target3 := &model.Target{Label: label.TargetLabel{Name: "target3"}}

	graph.AddVertex(target1)
	graph.AddVertex(target2)

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
		t.Errorf("AddEdge should have returned an error for non-existent 'to' vertex")
	}

	err = graph.AddEdge(target3, target1)
	if err == nil {
		t.Errorf("AddEdge should have returned an error for non-existent 'from' vertex")
	}

	if len(graph.outEdges[target1]) != 2 {
		t.Errorf("Expected 2 outEdges from target1, got %d", len(graph.outEdges[target1]))
	}

	if len(graph.inEdges[target2]) != 2 {
		t.Errorf("Expected 2 inEdges to target2, got %d", len(graph.inEdges[target2]))
	}
}

func TestDirectedTargetGraph_GetInEdges(t *testing.T) {
	graph := NewDirectedGraph()
	target1 := &model.Target{Label: label.TargetLabel{Name: "target1"}}
	target2 := &model.Target{Label: label.TargetLabel{Name: "target2"}}
	target3 := &model.Target{Label: label.TargetLabel{Name: "target3"}}

	graph.AddVertex(target1)
	graph.AddVertex(target2)
	graph.AddVertex(target3)

	graph.AddEdge(target1, target2)
	graph.AddEdge(target3, target2)

	inEdges, err := graph.GetInEdges(target2)
	if err != nil {
		t.Fatalf("GetInEdges failed: %v", err)
	}

	expectedInEdges := []*model.Target{target1, target3}
	if !reflect.DeepEqual(inEdges, expectedInEdges) {
		t.Errorf("GetInEdges returned incorrect inEdges. Expected %v, got %v", expectedInEdges, inEdges)
	}

	_, err = graph.GetInEdges(&model.Target{Label: label.TargetLabel{Name: "nonExistent"}})
	if err == nil {
		t.Errorf("GetInEdges should have returned an error for non-existent vertex")
	}
}

func TestDirectedTargetGraph_GetOutEdges(t *testing.T) {
	graph := NewDirectedGraph()
	target1 := &model.Target{Label: label.TargetLabel{Name: "target1"}}
	target2 := &model.Target{Label: label.TargetLabel{Name: "target2"}}
	target3 := &model.Target{Label: label.TargetLabel{Name: "target3"}}

	graph.AddVertex(target1)
	graph.AddVertex(target2)
	graph.AddVertex(target3)

	graph.AddEdge(target2, target1)
	graph.AddEdge(target2, target3)

	outEdges, err := graph.GetOutEdges(target2)
	if err != nil {
		t.Fatalf("GetOutEdges failed: %v", err)
	}

	expectedOutEdges := []*model.Target{target1, target3} //Two edges added in AddEdge test
	if !reflect.DeepEqual(outEdges, expectedOutEdges) {
		t.Errorf("GetOutEdges returned incorrect outEdges. Expected %v, got %v", expectedOutEdges, outEdges)
	}

	_, err = graph.GetOutEdges(&model.Target{Label: label.TargetLabel{Name: "nonExistent"}})
	if err == nil {
		t.Errorf("GetOutEdges should have returned an error for non-existent vertex")
	}
}

func TestDirectedTargetGraph_hasVertex(t *testing.T) {
	graph := NewDirectedGraph()
	target1 := &model.Target{Label: label.TargetLabel{Name: "target1"}}
	target2 := &model.Target{Label: label.TargetLabel{Name: "target2"}}

	graph.AddVertex(target1)

	if !graph.hasVertex(target1) {
		t.Errorf("hasVertex should have returned true for existing vertex")
	}

	if graph.hasVertex(target2) {
		t.Errorf("hasVertex should have returned false for non-existent vertex")
	}
}

func TestDirectedTargetGraph_HasCycle(t *testing.T) {
	graph := NewDirectedGraph()
	target1 := &model.Target{Label: label.TargetLabel{Name: "target1"}}
	target2 := &model.Target{Label: label.TargetLabel{Name: "target2"}}
	target3 := &model.Target{Label: label.TargetLabel{Name: "target3"}}

	graph.AddVertex(target1)
	graph.AddVertex(target2)
	graph.AddVertex(target3)

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
	graph.outEdges[target3] = []*model.Target{}

	// Check if there is a cycle
	graph = NewDirectedGraph()
	target1 = &model.Target{Label: label.TargetLabel{Name: "target1"}}
	target2 = &model.Target{Label: label.TargetLabel{Name: "target2"}}
	target3 = &model.Target{Label: label.TargetLabel{Name: "target3"}}

	graph.AddVertex(target1)
	graph.AddVertex(target2)
	graph.AddVertex(target3)

	graph.AddEdge(target1, target2)
	graph.AddEdge(target2, target3)

	if graph.HasCycle() {
		t.Errorf("HasCycle should have returned false for a graph without a cycle")
	}
}
