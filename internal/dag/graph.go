package dag

import (
	"encoding/json"
	"errors"
	"fmt"
	"go.uber.org/zap"
	"grog/internal/config"
	"grog/internal/label"
	"grog/internal/model"
	"slices"
	"strings"
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
	if !g.hasVertex(from) || !g.hasVertex(to) {
		return errors.New("both vertices must exist in the graph")
	}
	g.outEdges[from.Label] = append(g.outEdges[from.Label], to)
	g.inEdges[to.Label] = append(g.inEdges[to.Label], from)
	return nil
}

// GetInEdges returns a list of vertices pointing to the given vertex.
// In the context of build targets these would be the dependencies
func (g *DirectedTargetGraph) GetInEdges(target *model.Target) ([]*model.Target, error) {
	if !g.hasVertex(target) {
		return nil, errors.New("vertex not found")
	}
	return g.inEdges[target.Label], nil
}

// GetOutEdges returns a list of vertices pointing from the given vertex.
// In the context of build targets these would be the targets that depend on the given target.
func (g *DirectedTargetGraph) GetOutEdges(target *model.Target) ([]*model.Target, error) {
	if !g.hasVertex(target) {
		return nil, errors.New("vertex not found")
	}
	return g.outEdges[target.Label], nil
}

// GetDescendants returns a list of vertices that are descendants of the given vertex.
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

// SelectTargets sets targets as selected
// returns the number of selected targets, the number of targets skipped due to platform mismatch
// and an error if a selected target depends on a target that does not match the platform
func (g *DirectedTargetGraph) SelectTargets(
	pattern label.TargetPattern,
	tags []string,
	isTest bool,
) (int, int, error) {

	platformSkipped := 0
	for _, target := range g.vertices {

		hasTag := false
		for _, tag := range tags {
			if slices.Contains(target.Tags, tag) {
				hasTag = true
				break
			}
		}

		// Match pattern and test flag
		if pattern.Matches(target.Label) && (isTest == target.IsTest()) && (hasTag || len(tags) == 0) {
			if !targetMatchesPlatform(target) {
				platformSkipped += 1
				continue // Skip targets that don't match the platform
			}
			target.IsSelected = true
			if err := g.selectAllAncestors([]string{target.Label.String()}, target); err != nil {
				return 0, 0, err
			}
		}
	}

	// Doing it all in one loop would be faster, but this is easier to reason about
	selectedCount := 0
	for _, target := range g.vertices {
		if target.IsSelected {
			selectedCount++
		}
	}

	return selectedCount, platformSkipped, nil
}

// selectAllAncestors recursively selects all ancestors of the given target
// and returns the number of selected targets.
func (g *DirectedTargetGraph) selectAllAncestors(depChain []string, target *model.Target) error {
	for _, ancestor := range g.inEdges[target.Label] {
		depChain = append(depChain, ancestor.Label.String())
		if !targetMatchesPlatform(ancestor) {
			depChainStr := strings.Join(depChain[1:], " -> ")
			return fmt.Errorf("could not select target %s because it depends on %s, which does not match the platform %s",
				depChain[0], depChainStr, config.Global.GetPlatform())
		}

		ancestor.IsSelected = true
		if err := g.selectAllAncestors(depChain, ancestor); err != nil {
			return err
		}
	}
	return nil
}

func targetMatchesPlatform(target *model.Target) bool {
	if target.Platform == nil {
		return true
	}

	if len(target.Platform.OS) != 0 && !slices.Contains(target.Platform.OS, config.Global.OS) {
		return false
	}
	if len(target.Platform.Arch) != 0 && !slices.Contains(target.Platform.Arch, config.Global.Arch) {
		return false
	}

	return true
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
				From: from.String(),
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

// GraphJSON is a helper struct for JSON serialization
type GraphJSON struct {
	Vertices []*model.Target     `json:"vertices"`
	Edges    map[string][]string `json:"edges"` // from label to label
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
