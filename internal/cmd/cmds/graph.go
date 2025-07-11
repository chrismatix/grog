package cmds

import (
	"fmt"
	"github.com/charmbracelet/lipgloss"
	"grog/internal/console"
	"sort"

	"github.com/TyphonHill/go-mermaid/diagrams/flowchart"
	"github.com/charmbracelet/lipgloss/tree"
	"github.com/spf13/cobra"
	"grog/internal/analysis"
	"grog/internal/completions"
	"grog/internal/config"
	"grog/internal/dag"
	"grog/internal/label"
	"grog/internal/loading"
	"grog/internal/model"
	"grog/internal/selection"
	"path/filepath"
)

var graphOptions struct {
	output               string
	mermaidInputsAsNodes bool
	transitive           bool
}

var GraphCmd = &cobra.Command{
	Use:   "graph",
	Short: "Outputs the target dependency graph.",
	Long: `Visualizes the dependency graph of targets in various formats.
Supports tree, JSON, and Mermaid diagram output formats. By default, only direct dependencies are shown.`,
	Example: `  grog graph                                # Show dependency tree for all targets
  grog graph //path/to/package:target         # Show dependencies for a specific target
  grog graph -o mermaid //path/to/package:target  # Output as Mermaid diagram
  grog graph -t //path/to/package:target      # Include transitive dependencies`,
	Args:              cobra.ArbitraryArgs,
	ValidArgsFunction: completions.AllTargetPatternCompletion,
	Run: func(cmd *cobra.Command, args []string) {
		ctx, logger := console.SetupCommand()

		packages, err := loading.LoadAllPackages(ctx)
		if err != nil {
			logger.Fatalf(
				"could not load packages: %v",
				err)
		}

		currentPackagePath, err := config.Global.GetCurrentPackage()
		if err != nil {
			logger.Fatalf("could not get current package: %v", err)
		}

		targetPatterns, err := label.ParsePatternsOrMatchAll(currentPackagePath, args)
		if err != nil {
			logger.Fatalf("could not parse target pattern: %v", err)
		}

		nodes, err := model.BuildNodeMapFromPackages(packages)
		if err != nil {
			logger.Fatalf("could not create target map: %v", err)
		}

		graph, err := analysis.BuildGraph(nodes)
		if err != nil {
			logger.Fatalf("could not build graph: %v", err)
		}

		// TODO make this explicitly configurable
		// Graphing by default should ignore platform selectors as it is more about documentation
		// and not execution.
		config.Global.AllPlatforms = true
		selector := selection.New(targetPatterns, config.Global.Tags, config.Global.ExcludeTags, selection.AllTargets)
		selector.SelectTargets(graph)

		if graphOptions.transitive {
			// Select transitive dependencies of the selected nodes
			for _, target := range graph.GetSelectedNodes() {
				for _, ancestor := range graph.GetAncestors(target) {
					ancestor.Select()
				}
			}
		}

		subgraph := graph.GetSelectedSubgraph()

		if graphOptions.output == "mermaid" {
			printMermaidDiagram(subgraph)
		} else if graphOptions.output == "tree" {
			printTree(subgraph)
		} else if graphOptions.output == "json" {
			jsonData, err := subgraph.MarshalJSON()
			if err != nil {
				logger.Fatalf("could not marshal graph to json: %v", err)
			}
			fmt.Println(string(jsonData))
		} else {
			logger.Fatalf("unknown output format: %s", graphOptions.output)
		}
	},
}

func AddGraphCmd(cmd *cobra.Command) {
	GraphCmd.Flags().BoolVarP(
		&graphOptions.transitive,
		"transitive",
		"t",
		false,
		"Include all transitive dependencies of the selected targets.")

	GraphCmd.Flags().StringVarP(&graphOptions.output, "output", "o", "tree", "Output format. One of: tree, json, mermaid.")
	GraphCmd.Flags().BoolVarP(&graphOptions.mermaidInputsAsNodes, "mermaid-inputs-as-nodes", "m", false, "Render inputs as nodes in mermaid graphs.")

	cmd.AddCommand(GraphCmd)
}

func printMermaidDiagram(graph *dag.DirectedTargetGraph) {
	chart := flowchart.NewFlowchart()
	chart.SetDirection(flowchart.FlowchartDirectionBottomUp)

	workspaceDir := config.Global.WorkspaceRoot

	chart.Title = fmt.Sprintf("%s dependency graph", filepath.Base(workspaceDir))

	// Add Nodes:
	// Deterministic ordering of nodes.
	nodes := graph.GetNodes().SelectedNodesAlphabetically()
	nodeMap := make(map[string]*flowchart.Node)
	for _, node := range nodes {
		chartNode := chart.AddNode(node.GetLabel().String())
		chartNode.Style = &flowchart.NodeStyle{
			Fill:        "#E3F2FD", // light-blue-50
			Stroke:      "#1E88E5", // blue-600
			StrokeWidth: 2,
		}
		nodeMap[node.GetLabel().String()] = chartNode

		targetNode, isTarget := node.(*model.Target)
		// Add inputs as separate nodes if requested.
		if graphOptions.mermaidInputsAsNodes && isTarget {
			// Keep input nodes in stable order as well.
			inputs := make([]string, len(targetNode.UnresolvedInputs))
			copy(inputs, targetNode.UnresolvedInputs)
			sort.Strings(inputs)

			for _, input := range inputs {
				inputNode := chart.AddNode(input)
				inputNode.Style = &flowchart.NodeStyle{
					Fill:        "#FFF8E1", // amber-50
					Stroke:      "#FB8C00", // orange-600
					StrokeWidth: 2,
					StrokeDash:  "6 3", // dashed border emphasises "just an input"
				}
				link := chart.AddLink(inputNode, chartNode)
				link.Shape = flowchart.LinkShapeDotted
			}
		}
	}

	// Add Edges:
	out := graph.GetOutEdges()

	// Sort the map keys.
	var fromKeys []label.TargetLabel
	for key := range out {
		fromKeys = append(fromKeys, key)
	}
	sort.Slice(fromKeys, func(i, j int) bool {
		return fromKeys[i].String() < fromKeys[j].String()
	})

	for _, from := range fromKeys {
		toList := out[from]
		// Sort each destination slice.
		sort.Slice(toList, func(i, j int) bool {
			return toList[i].GetLabel().String() < toList[j].GetLabel().String()
		})

		for _, to := range toList {
			chart.AddLink(nodeMap[from.String()], nodeMap[to.GetLabel().String()])
		}
	}

	fmt.Println(chart.String())
}

func printTree(graph *dag.DirectedTargetGraph) {
	// Gather all nodes that have no dependants – they are the roots.
	var roots []model.BuildNode
	for _, v := range graph.GetNodes() {
		if len(graph.GetDependants(v)) == 0 {
			roots = append(roots, v)
		}
	}

	// Deterministic order of roots.
	sort.Slice(roots, func(i, j int) bool {
		return roots[i].GetLabel().String() < roots[j].GetLabel().String()
	})

	for _, root := range roots {
		rootTree := buildTree(root, graph, 0)
		fmt.Println(rootTree)
	}
}

// buildTree converts a node and all of its (transitive) dependencies into a
// Lipgloss tree.
func buildTree(
	node model.BuildNode,
	graph *dag.DirectedTargetGraph,
	level int,
) *tree.Tree {
	targetLabel := node.GetLabel().String()
	t := tree.New().Root(targetLabel)

	if level == 0 {
		enumeratorStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("63")).MarginRight(1)
		rootStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("35"))
		itemStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("212"))
		t.RootStyle(rootStyle).ItemStyle(itemStyle).EnumeratorStyle(enumeratorStyle).Enumerator(tree.RoundedEnumerator)
	}

	// Deterministic output – sort the direct dependencies alphabetically.
	deps := graph.GetDependencies(node)
	sort.Slice(deps, func(i, j int) bool {
		return deps[i].GetLabel().String() < deps[j].GetLabel().String()
	})

	for _, dep := range deps {
		depLabel := dep.GetLabel().String()

		if len(graph.GetDependencies(dep)) > 0 {
			t.Child(buildTree(dep, graph, level+1))
		} else {
			t.Child(depLabel)
		}
	}

	return t
}
