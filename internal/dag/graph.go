package dag

import (
	"encoding/json"
	"errors"
	"fmt"
	"go.uber.org/zap"
	"grog/internal/label"
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
		return fmt.Errorf("cannot add self-loop for target %s", from.Label)
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

// GetDescendants returns a list of vertices that are descendants of the given vertex.
// Recurses via the outEdges of each vertex.
func (g *DirectedTargetGraph) GetDescendants(target *model.Target) []*model.Target {
	var descendants []*model.Target
	for _, descendant := range g.outEdges[target] {
		descendants = append(descendants, descendant)

		// Recurse
		recursiveDescendants := g.GetDescendants(descendant)
		descendants = append(descendants, recursiveDescendants...)
	}
	return descendants
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

// SelectTargets sets targets as selected and returns the number of selected targets.
func (g *DirectedTargetGraph) SelectTargets(pattern label.TargetPattern, isTest bool) int {
	selectedCount := 0
	for _, target := range g.vertices {
		// Match pattern and test flag
		if pattern.Matches(target.Label) && (isTest == target.IsTest()) {
			target.IsSelected = true
			selectedCount++
			selectedCount += g.selectAllAncestors(target)
		}
	}

	return selectedCount
}

// selectAllAncestors recursively selects all ancestors of the given target
// and returns the number of selected targets.
func (g *DirectedTargetGraph) selectAllAncestors(target *model.Target) int {
	selectedCount := 0
	for _, ancestor := range g.inEdges[target] {
		if ancestor.IsSelected {
			// Already selected, skip
			continue
		}
		ancestor.IsSelected = true
		selectedCount++
		selectedCount += g.selectAllAncestors(ancestor)
	}
	return selectedCount
}

// SelectAllTargets selects all targets in the graphs
func (g *DirectedTargetGraph) SelectAllTargets() {
	for _, target := range g.vertices {
		target.IsSelected = true
	}
}

// ToJSON serializes the directed graph to a JSON representation for debugging.
func (g *DirectedTargetGraph) ToJSON() (string, error) {
	type Edge struct {
		From string `json:"from"`
		To   string `json:"to"`
	}

	type GraphJSON struct {
		Vertices []string `json:"vertices"`
		Edges    []Edge   `json:"edges"`
	}

	graphJSON := GraphJSON{
		Vertices: make([]string, 0, len(g.vertices)),
		Edges:    []Edge{},
	}

	// Add vertices
	for _, vertex := range g.vertices {
		graphJSON.Vertices = append(graphJSON.Vertices, vertex.Label.String())
	}

	// Add edges
	for from, toList := range g.outEdges {
		for _, to := range toList {
			graphJSON.Edges = append(graphJSON.Edges, Edge{
				From: from.Label.String(),
				To:   to.Label.String(),
			})
		}
	}

	// Serialize to JSON
	jsonData, err := json.MarshalIndent(graphJSON, "", "  ")
	if err != nil {
		return "", fmt.Errorf("failed to serialize graph to JSON: %w", err)
	}

	return string(jsonData), nil
}

func (g *DirectedTargetGraph) LogGraphJSON(logger *zap.SugaredLogger) {
	jsonStr, err := g.ToJSON()
	if err != nil {
		logger.Debugf("failed to serialize graph to JSON: %v", err)
		return
	}
	logger.Debugf("graph: %s", jsonStr)
}
