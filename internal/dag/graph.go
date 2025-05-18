package dag

import (
	"encoding/json"
	"errors"
	"fmt"
	"grog/internal/label"
	"grog/internal/model"
)

// DirectedTargetGraph represents a directed graph of build targets.
// It is used to represent the dependency graph of a project.
// In order to assert that it does not contain cycles, you can call hasCycle()
type DirectedTargetGraph struct {
	vertices model.TargetMap

	// outEdges maps a vertex to its list of outgoing vertices,
	// representing the directed outEdges in the graph.
	outEdges map[label.TargetLabel][]*model.Target
	// inEdges map a vertex to its list of incoming vertices,
	// representing the directed inEdges in the graph.
	inEdges map[label.TargetLabel][]*model.Target
}

// NewDirectedGraph creates and initializes a new directed graph.
func NewDirectedGraph() *DirectedTargetGraph {
	return &DirectedTargetGraph{
		vertices: make(model.TargetMap),
		outEdges: make(map[label.TargetLabel][]*model.Target),
		inEdges:  make(map[label.TargetLabel][]*model.Target),
	}
}

func NewDirectedGraphFromMap(targetMap model.TargetMap) *DirectedTargetGraph {
	return &DirectedTargetGraph{
		vertices: targetMap,
		outEdges: make(map[label.TargetLabel][]*model.Target),
		inEdges:  make(map[label.TargetLabel][]*model.Target),
	}
}

// NewDirectedGraphFromTargets Useful for testing
func NewDirectedGraphFromTargets(targets ...*model.Target) *DirectedTargetGraph {
	return &DirectedTargetGraph{
		vertices: model.TargetMapFromTargets(targets...),
		outEdges: make(map[label.TargetLabel][]*model.Target),
		inEdges:  make(map[label.TargetLabel][]*model.Target),
	}
}

func (g *DirectedTargetGraph) GetVertices() model.TargetMap {
	return g.vertices
}

func (g *DirectedTargetGraph) GetSelectedVertices() []*model.Target {
	// Filter selected vertices and return them
	var selectedVertices []*model.Target
	for _, vertex := range g.vertices {
		if vertex.IsSelected {
			selectedVertices = append(selectedVertices, vertex)
		}
	}
	return selectedVertices
}

// AddVertex idempotently adds a new vertex to the graph.
func (g *DirectedTargetGraph) AddVertex(target *model.Target) {
	if !g.hasVertex(target) {
		g.vertices[target.Label] = target
	}
}

// AddEdge adds a directed edge between two vertices.
func (g *DirectedTargetGraph) AddEdge(from, to *model.Target) error {
	if from == to {
		return fmt.Errorf("cannot add self-loop for target %s", from.Label)
	}
	if !g.hasVertex(from) {
		return fmt.Errorf("vertex %s does not exist in the graph", from.Label)
	}
	if !g.hasVertex(to) {
		return fmt.Errorf("vertex %s does not exist in the graph", from.Label)
	}
	g.outEdges[from.Label] = append(g.outEdges[from.Label], to)
	g.inEdges[to.Label] = append(g.inEdges[to.Label], from)
	return nil
}

func (g *DirectedTargetGraph) GetDependencies(target *model.Target) ([]*model.Target, error) {
	if !g.hasVertex(target) {
		return nil, errors.New("vertex not found")
	}
	return g.inEdges[target.Label], nil
}

func (g *DirectedTargetGraph) GetDependants(target *model.Target) ([]*model.Target, error) {
	if !g.hasVertex(target) {
		return nil, errors.New("vertex not found")
	}
	return g.outEdges[target.Label], nil
}

// GetDescendants returns a list of vertices that are descendants (dependants) of the given vertex.
// Recurses via the outEdges of each vertex.
func (g *DirectedTargetGraph) GetDescendants(target *model.Target) []*model.Target {
	var descendants []*model.Target
	for _, descendant := range g.outEdges[target.Label] {
		descendants = append(descendants, descendant)

		// Recurse
		recursiveDescendants := g.GetDescendants(descendant)
		descendants = append(descendants, recursiveDescendants...)
	}
	return descendants
}

// GetAncestors returns a list of vertices that are ancestors (dependencies) of the given vertex.
// // Recurses via the inEdges of each vertex.
func (g *DirectedTargetGraph) GetAncestors(target *model.Target) []*model.Target {
	var ancestors []*model.Target
	for _, ancestor := range g.inEdges[target.Label] {
		ancestors = append(ancestors, ancestor)

		// Recurse
		recursiveAncestors := g.GetAncestors(ancestor)
		ancestors = append(ancestors, recursiveAncestors...)
	}
	return ancestors
}

// hasVertex checks whether a vertex exists in the graph.
func (g *DirectedTargetGraph) hasVertex(target *model.Target) bool {
	if target == nil {
		return false
	}
	return g.vertices[target.Label] != nil
}

// HasCycle detects if the directed graph has a cycle using Depth-First Search (DFS).
// It maintains three states for each vertex:
// - 0: unvisited
// - 1: visiting (currently in the recursion stack)
// - 2: visited (completely explored)
func (g *DirectedTargetGraph) HasCycle() bool {
	visited := make(map[*model.Target]int) // 0: unvisited, 1: visiting, 2: visited

	var depthFirstSearch func(target *model.Target) bool

	depthFirstSearch = func(target *model.Target) bool {
		visited[target] = 1 // Mark as visiting

		for _, neighbor := range g.outEdges[target.Label] {
			if visited[neighbor] == 1 {
				return true // Cycle detected
			}
			if visited[neighbor] == 0 {
				if depthFirstSearch(neighbor) {
					return true // Cycle detected in descendant
				}
			}
		}

		visited[target] = 2 // Mark as visited
		return false        // No cycle detected in this branch
	}

	for _, vertex := range g.vertices {
		if visited[vertex] == 0 {
			if depthFirstSearch(vertex) {
				return true // Cycle detected starting from this vertex
			}
		}
	}

	return false // No cycle detected in the entire graph
}

// GraphJSON is a helper struct for JSON serialization
type GraphJSON struct {
	Vertices []*model.Target     `json:"vertices"`
	Edges    map[string][]string `json:"edges"` // from label to label
}

func (g *DirectedTargetGraph) LogSelectedVertices() {
	for _, vertex := range g.vertices.SelectedTargetsAlphabetically() {
		fmt.Println(vertex.Label)
	}
}

// MarshalJSON serializes the graph to JSON
func (g *DirectedTargetGraph) MarshalJSON() ([]byte, error) {
	graphJSON := GraphJSON{
		Vertices: g.vertices.TargetsAlphabetically(),
		Edges:    map[string][]string{},
	}

	for from, toList := range g.outEdges {
		fromLabel := from
		var toLabels []string
		for _, to := range toList {
			toLabels = append(toLabels, to.Label.String())
		}
		graphJSON.Edges[fromLabel.String()] = toLabels
	}

	return json.Marshal(graphJSON)
}
