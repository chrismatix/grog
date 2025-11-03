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
	nodes model.BuildNodeMap

	// outEdges maps a node to its list of outgoing nodes,
	// representing the directed outEdges in the graph.
	outEdges map[label.TargetLabel][]model.BuildNode
	// inEdges map a node to its list of incoming nodes,
	// representing the directed inEdges in the graph.
	inEdges map[label.TargetLabel][]model.BuildNode
}

// NewDirectedGraph creates and initializes a new directed graph.
func NewDirectedGraph() *DirectedTargetGraph {
	return &DirectedTargetGraph{
		nodes:    make(model.BuildNodeMap),
		outEdges: make(map[label.TargetLabel][]model.BuildNode),
		inEdges:  make(map[label.TargetLabel][]model.BuildNode),
	}
}

func NewDirectedGraphFromMap(targetMap model.BuildNodeMap) *DirectedTargetGraph {
	return &DirectedTargetGraph{
		nodes:    targetMap,
		outEdges: make(map[label.TargetLabel][]model.BuildNode),
		inEdges:  make(map[label.TargetLabel][]model.BuildNode),
	}
}

// NewDirectedGraphFromTargets Useful for testing
func NewDirectedGraphFromTargets(nodes ...model.BuildNode) *DirectedTargetGraph {
	return &DirectedTargetGraph{
		nodes:    model.BuildNodeMapFromNodes(nodes...),
		outEdges: make(map[label.TargetLabel][]model.BuildNode),
		inEdges:  make(map[label.TargetLabel][]model.BuildNode),
	}
}

func (g *DirectedTargetGraph) GetNodes() model.BuildNodeMap {
	return g.nodes
}

func (g *DirectedTargetGraph) GetOutEdges() map[label.TargetLabel][]model.BuildNode {
	return g.outEdges
}

func (g *DirectedTargetGraph) GetSelectedNodes() []model.BuildNode {
	// Filter selected nodes and return them
	var selectedNodes []model.BuildNode
	for _, node := range g.nodes {
		if node.GetIsSelected() {
			selectedNodes = append(selectedNodes, node)
		}
	}
	return selectedNodes
}

// GetSelectedSubgraph returns a new graph containing only selected nodes and edges between them.
// The returned graph preserves the edge relationships between selected nodes from the original graph.
func (g *DirectedTargetGraph) GetSelectedSubgraph() *DirectedTargetGraph {
	subgraph := NewDirectedGraph()

	// Add all selected nodes
	for _, node := range g.nodes {
		if node.GetIsSelected() {
			subgraph.AddNode(node)
		}
	}

	// Add edges between selected nodes
	for fromLabel, toList := range g.outEdges {
		if g.nodes[fromLabel].GetIsSelected() {
			for _, to := range toList {
				if to.GetIsSelected() {
					subgraph.AddEdge(g.nodes[fromLabel], to)
				}
			}
		}
	}

	return subgraph
}

// AddNode idempotently adds a new node to the graph.
func (g *DirectedTargetGraph) AddNode(node model.BuildNode) {
	if !g.hasNode(node) {
		g.nodes[node.GetLabel()] = node
	}
}

// AddEdge adds a directed edge between two nodes.
func (g *DirectedTargetGraph) AddEdge(from, to model.BuildNode) error {
	if from == to {
		return fmt.Errorf("cannot add self-loop for target %s", from.GetLabel())
	}
	if !g.hasNode(from) {
		return fmt.Errorf("node %s does not exist in the graph", from.GetLabel())
	}
	if !g.hasNode(to) {
		return fmt.Errorf("node %s does not exist in the graph", from.GetLabel())
	}
	g.outEdges[from.GetLabel()] = append(g.outEdges[from.GetLabel()], to)
	g.inEdges[to.GetLabel()] = append(g.inEdges[to.GetLabel()], from)
	return nil
}

func (g *DirectedTargetGraph) GetDependencies(target model.BuildNode) []model.BuildNode {
	return g.inEdges[target.GetLabel()]
}

func (g *DirectedTargetGraph) GetTargetDependencies(node model.BuildNode) []*model.Target {
	var targets []*model.Target
	for _, dependency := range g.GetDependencies(node) {
		if target, ok := dependency.(*model.Target); ok {
			targets = append(targets, target)
		}
	}
	return targets
}

func (g *DirectedTargetGraph) GetDependants(target model.BuildNode) []model.BuildNode {
	return g.outEdges[target.GetLabel()]
}

// GetDescendants returns a list of nodes that are descendants (dependants) of the given node.
// Recurses via the outEdges of each node.
func (g *DirectedTargetGraph) GetDescendants(target model.BuildNode) []model.BuildNode {
	var descendants []model.BuildNode
	for _, descendant := range g.outEdges[target.GetLabel()] {
		descendants = append(descendants, descendant)

		// Recurse
		recursiveDescendants := g.GetDescendants(descendant)
		descendants = append(descendants, recursiveDescendants...)
	}
	return descendants
}

// GetAncestors returns a list of nodes that are ancestors (transitive dependencies) of the given node.
// Recurses via the inEdges of each node.
func (g *DirectedTargetGraph) GetAncestors(target model.BuildNode) []model.BuildNode {
	var ancestors []model.BuildNode
	for _, ancestor := range g.inEdges[target.GetLabel()] {
		ancestors = append(ancestors, ancestor)

		// Recurse
		recursiveAncestors := g.GetAncestors(ancestor)
		ancestors = append(ancestors, recursiveAncestors...)
	}
	return ancestors
}

// hasNode checks whether a node exists in the graph.
func (g *DirectedTargetGraph) hasNode(node model.BuildNode) bool {
	if node == nil {
		return false
	}
	return g.nodes[node.GetLabel()] != nil
}

// HasCycle detects if the directed graph has a cycle using Depth-First Search (DFS).
// It maintains three states for each node:
// - 0: unvisited
// - 1: visiting (currently in the recursion stack)
// - 2: visited (completely explored)
func (g *DirectedTargetGraph) HasCycle() bool {
	_, hasCycle := g.FindCycle()
	return hasCycle
}

// FindCycle returns the nodes that form a cycle in the graph, including the repeated
// starting node at the end of the slice to illustrate the full loop. The boolean return
// value indicates whether a cycle was found.
func (g *DirectedTargetGraph) FindCycle() ([]model.BuildNode, bool) {
	visited := make(map[model.BuildNode]int) // 0: unvisited, 1: visiting, 2: visited
	var stack []model.BuildNode
	var cycle []model.BuildNode

	var depthFirstSearch func(target model.BuildNode) bool

	depthFirstSearch = func(target model.BuildNode) bool {
		visited[target] = 1 // Mark as visiting
		stack = append(stack, target)

		for _, neighbor := range g.outEdges[target.GetLabel()] {
			if visited[neighbor] == 0 {
				if depthFirstSearch(neighbor) {
					return true // Cycle detected in descendant
				}
				continue
			}
			if visited[neighbor] == 1 {
				idx := -1
				for i := len(stack) - 1; i >= 0; i-- {
					if stack[i] == neighbor {
						idx = i
						break
					}
				}
				if idx == -1 {
					cycle = []model.BuildNode{neighbor, neighbor}
				} else {
					cycle = append([]model.BuildNode{}, stack[idx:]...)
					cycle = append(cycle, neighbor)
				}
				return true
			}
		}

		stack = stack[:len(stack)-1]
		visited[target] = 2 // Mark as visited
		return false        // No cycle detected in this branch
	}

	for _, node := range g.nodes.NodesAlphabetically() {
		if visited[node] == 0 {
			if depthFirstSearch(node) {
				return cycle, true // Cycle detected starting from this node
			}
		}
	}

	return nil, false // No cycle detected in the entire graph
}

// GraphJSON is a helper struct for JSON serialization
type GraphJSON struct {
	Nodes []model.BuildNode   `json:"nodes"`
	Edges map[string][]string `json:"edges"` // from label to label
}

func (g *DirectedTargetGraph) LogSelectedNodes() {
	for _, node := range g.nodes.SelectedNodesAlphabetically() {
		fmt.Println(node.GetLabel())
	}
}

// MarshalJSON serializes the graph to JSON
func (g *DirectedTargetGraph) MarshalJSON() ([]byte, error) {
	graphJSON := GraphJSON{
		Nodes: g.nodes.NodesAlphabetically(),
		Edges: map[string][]string{},
	}

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
