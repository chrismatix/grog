package dag

import (
	"errors"
	"grog/internal/model"
)

// DirectedTargetGraph represents a directed graph of build targets.
// It is used to represent the dependency graph of a project.
// In order to assert that it does not contain cycles, you can call hasCycle()
type DirectedTargetGraph struct {
	vertices []*model.Target

	// outEdges maps a vertex to its list of outgoing vertices,
	// representing the directed outEdges in the graph.
	outEdges map[*model.Target][]*model.Target
	// inEdges map a vertex to its list of incoming vertices,
	// representing the directed inEdges in the graph.
	inEdges map[*model.Target][]*model.Target
}

// NewDirectedGraph creates and initializes a new directed graph.
func NewDirectedGraph() *DirectedTargetGraph {
	return &DirectedTargetGraph{
		vertices: []*model.Target{},
		outEdges: make(map[*model.Target][]*model.Target),
		inEdges:  make(map[*model.Target][]*model.Target),
	}
}

// AddVertex idempotently adds a new vertex to the graph.
func (g *DirectedTargetGraph) AddVertex(target *model.Target) {
	for _, vertex := range g.vertices {
		if vertex == target {
			return
		}
	}
	g.vertices = append(g.vertices, target)
}

// AddEdge adds a directed edge between two vertices.
func (g *DirectedTargetGraph) AddEdge(from, to *model.Target) error {
	if from == to {
		return errors.New("cannot add self-loop")
	}
	if !g.hasVertex(from) || !g.hasVertex(to) {
		return errors.New("both vertices must exist in the graph")
	}
	g.outEdges[from] = append(g.outEdges[from], to)
	g.inEdges[to] = append(g.inEdges[to], from)
	return nil
}

// GetInEdges returns a list of vertices pointing to the given vertex.
// In the context of build targets these would be the dependencies
func (g *DirectedTargetGraph) GetInEdges(target *model.Target) ([]*model.Target, error) {
	if !g.hasVertex(target) {
		return nil, errors.New("vertex not found")
	}
	return g.inEdges[target], nil
}

// GetOutEdges returns a list of vertices pointing from the given vertex.
// In the context of build targets these would be the targets that depend on the given target.
func (g *DirectedTargetGraph) GetOutEdges(target *model.Target) ([]*model.Target, error) {
	if !g.hasVertex(target) {
		return nil, errors.New("vertex not found")
	}
	return g.outEdges[target], nil
}

// hasVertex checks whether a vertex exists in the graph.
func (g *DirectedTargetGraph) hasVertex(target *model.Target) bool {
	for _, vertex := range g.vertices {
		if vertex == target {
			return true
		}
	}
	return false
}

// HasCycle detects if the directed graph has a cycle using Depth-First Search (DFS).
// It maintains three states for each vertex:
// - 0: unvisited
// - 1: visiting (currently in the recursion stack)
// - 2: visited (completely explored)
func (g *DirectedTargetGraph) HasCycle() bool {
	visited := make(map[*model.Target]int) // 0: unvisited, 1: visiting, 2: visited

	var dfs func(target *model.Target) bool

	dfs = func(target *model.Target) bool {
		visited[target] = 1 // Mark as visiting

		for _, neighbor := range g.outEdges[target] {
			if visited[neighbor] == 1 {
				return true // Cycle detected
			}
			if visited[neighbor] == 0 {
				if dfs(neighbor) {
					return true // Cycle detected in descendant
				}
			}
		}

		visited[target] = 2 // Mark as visited
		return false        // No cycle detected in this branch
	}

	for _, vertex := range g.vertices {
		if visited[vertex] == 0 {
			if dfs(vertex) {
				return true // Cycle detected starting from this vertex
			}
		}
	}

	return false // No cycle detected in the entire graph
}
