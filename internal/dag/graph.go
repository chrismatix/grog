package dag

import (
	"encoding/json"
	"fmt"
	"grog/internal/label"
	"grog/internal/model"
	"sort"
)

// DirectedTargetGraph represents a directed graph of build targets.
// It is used to represent the dependency graph of a project.
// In order to assert that it does not contain cycles, you can call hasCycle()
type DirectedTargetGraph struct {
	vertices model.BuildNodeMap

	// outEdges maps a vertex to its list of outgoing vertices,
	// representing the directed edges in the graph.
	outEdges map[label.TargetLabel][]model.BuildNode
	// inEdges map a vertex to its list of incoming vertices,
	// representing the directed edges in the graph.
	inEdges map[label.TargetLabel][]model.BuildNode
}

// NewDirectedGraph creates and initializes a new directed graph.
func NewDirectedGraph() *DirectedTargetGraph {
	return &DirectedTargetGraph{
		vertices: make(model.BuildNodeMap),
		outEdges: make(map[label.TargetLabel][]model.BuildNode),
		inEdges:  make(map[label.TargetLabel][]model.BuildNode),
	}
}

func NewDirectedGraphFromMap(nodeMap model.BuildNodeMap) *DirectedTargetGraph {
	return &DirectedTargetGraph{
		vertices: nodeMap,
		outEdges: make(map[label.TargetLabel][]model.BuildNode),
		inEdges:  make(map[label.TargetLabel][]model.BuildNode),
	}
}

// NewDirectedGraphFromTargets Useful for testing
func NewDirectedGraphFromTargets(targets ...*model.Target) *DirectedTargetGraph {
	return &DirectedTargetGraph{
		vertices: model.BuildNodeMapFromTargets(targets...),
		outEdges: make(map[label.TargetLabel][]model.BuildNode),
		inEdges:  make(map[label.TargetLabel][]model.BuildNode),
	}
}

func (g *DirectedTargetGraph) GetVertices() model.TargetMap {
	targets := make(model.TargetMap)
	for label, node := range g.vertices {
		if t, ok := node.(*model.Target); ok {
			targets[label] = t
		}
	}
	return targets
}

func (g *DirectedTargetGraph) GetOutEdges() map[label.TargetLabel][]model.BuildNode {
	return g.outEdges
}

func (g *DirectedTargetGraph) GetSelectedVertices() []*model.Target {
	// Filter selected vertices and return them
	var selectedVertices []*model.Target
	for _, node := range g.vertices {
		if t, ok := node.(*model.Target); ok {
			if t.IsSelected {
				selectedVertices = append(selectedVertices, t)
			}
		}
	}
	return selectedVertices
}

// GetSelectedSubgraph returns a new graph containing only selected vertices and edges between them.
// The returned graph preserves the edge relationships between selected vertices from the original graph.
func (g *DirectedTargetGraph) GetSelectedSubgraph() *DirectedTargetGraph {
	subgraph := NewDirectedGraph()

	// Add all selected vertices
	for _, node := range g.vertices {
		if t, ok := node.(*model.Target); ok {
			if t.IsSelected {
				subgraph.AddVertex(t)
			}
		}
	}

	// Add edges between selected vertices
	for fromLabel, toList := range g.outEdges {
		fromNode := g.vertices[fromLabel]
		fromTarget, ok := fromNode.(*model.Target)
		if ok && fromTarget.IsSelected {
			for _, to := range toList {
				if t, ok := to.(*model.Target); ok && t.IsSelected {
					subgraph.AddEdge(fromTarget, t)
				}
			}
		}
	}

	return subgraph
}

// AddVertex idempotently adds a new vertex to the graph.
func (g *DirectedTargetGraph) AddVertex(node model.BuildNode) {
	if !g.hasVertex(node) {
		g.vertices[node.GetLabel()] = node
	}
}

// AddEdge adds a directed edge between two vertices.
func (g *DirectedTargetGraph) AddEdge(from, to model.BuildNode) error {
	if from == to {
		return fmt.Errorf("cannot add self-loop for target %s", from.GetLabel())
	}
	if !g.hasVertex(from) {
		return fmt.Errorf("vertex %s does not exist in the graph", from.GetLabel())
	}
	if !g.hasVertex(to) {
		return fmt.Errorf("vertex %s does not exist in the graph", from.GetLabel())
	}
	g.outEdges[from.GetLabel()] = append(g.outEdges[from.GetLabel()], to)
	g.inEdges[to.GetLabel()] = append(g.inEdges[to.GetLabel()], from)
	return nil
}

func (g *DirectedTargetGraph) GetDependencies(target *model.Target) []*model.Target {
	var deps []*model.Target
	for _, dep := range g.inEdges[target.Label] {
		if t, ok := dep.(*model.Target); ok {
			deps = append(deps, t)
		}
	}
	return deps
}

func (g *DirectedTargetGraph) GetDependants(target *model.Target) []*model.Target {
	var deps []*model.Target
	for _, dep := range g.outEdges[target.Label] {
		if t, ok := dep.(*model.Target); ok {
			deps = append(deps, t)
		}
	}
	return deps
}

// GetDescendants returns a list of vertices that are descendants (dependants) of the given vertex.
// Recurses via the outEdges of each vertex.
func (g *DirectedTargetGraph) GetDescendants(target *model.Target) []*model.Target {
	var descendants []*model.Target
	for _, descendant := range g.outEdges[target.Label] {
		if t, ok := descendant.(*model.Target); ok {
			descendants = append(descendants, t)
			recursiveDescendants := g.GetDescendants(t)
			descendants = append(descendants, recursiveDescendants...)
		}
	}
	return descendants
}

// GetAncestors returns a list of vertices that are ancestors (transitive dependencies) of the given vertex.
// Recurses via the inEdges of each vertex.
func (g *DirectedTargetGraph) GetAncestors(target *model.Target) []*model.Target {
	var ancestors []*model.Target
	for _, ancestor := range g.inEdges[target.Label] {
		if t, ok := ancestor.(*model.Target); ok {
			ancestors = append(ancestors, t)
			recursiveAncestors := g.GetAncestors(t)
			ancestors = append(ancestors, recursiveAncestors...)
		}
	}
	return ancestors
}

// hasVertex checks whether a vertex exists in the graph.
func (g *DirectedTargetGraph) hasVertex(node model.BuildNode) bool {
	if node == nil {
		return false
	}
	return g.vertices[node.GetLabel()] != nil
}

// HasCycle detects if the directed graph has a cycle using Depth-First Search (DFS).
// It maintains three states for each vertex:
// - 0: unvisited
// - 1: visiting (currently in the recursion stack)
// - 2: visited (completely explored)
func (g *DirectedTargetGraph) HasCycle() bool {
	visited := make(map[model.BuildNode]int) // 0: unvisited, 1: visiting, 2: visited

	var depthFirstSearch func(node model.BuildNode) bool

	depthFirstSearch = func(node model.BuildNode) bool {
		visited[node] = 1 // Mark as visiting

		for _, neighbor := range g.outEdges[node.GetLabel()] {
			if visited[neighbor] == 1 {
				return true // Cycle detected
			}
			if visited[neighbor] == 0 {
				if depthFirstSearch(neighbor) {
					return true // Cycle detected in descendant
				}
			}
		}

		visited[node] = 2 // Mark as visited
		return false      // No cycle detected in this branch
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
	Vertices []string            `json:"vertices"`
	Edges    map[string][]string `json:"edges"` // from label to label
}

func (g *DirectedTargetGraph) LogSelectedVertices() {
	for _, node := range g.vertices {
		if t, ok := node.(*model.Target); ok && t.IsSelected {
			fmt.Println(t.Label)
		}
	}
}

// MarshalJSON serializes the graph to JSON
func (g *DirectedTargetGraph) MarshalJSON() ([]byte, error) {
	graphJSON := GraphJSON{
		Vertices: []string{},
		Edges:    map[string][]string{},
	}

	for _, node := range g.vertices {
		graphJSON.Vertices = append(graphJSON.Vertices, node.GetLabel().String())
	}
	sort.Strings(graphJSON.Vertices)

	for from, toList := range g.outEdges {
		fromLabel := from
		var toLabels []string
		for _, to := range toList {
			toLabels = append(toLabels, to.GetLabel().String())
		}
		sort.Strings(toLabels)
		graphJSON.Edges[fromLabel.String()] = toLabels
	}

	return json.Marshal(graphJSON)
}
