package cmds

import (
	"fmt"
	"github.com/charmbracelet/lipgloss"
	"sort"

	"github.com/TyphonHill/go-mermaid/diagrams/flowchart"
	"github.com/charmbracelet/lipgloss/tree"
	"github.com/spf13/cobra"
	"grog/internal/analysis"
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
	Run: func(cmd *cobra.Command, args []string) {
		ctx, logger := setupCommand()

		packages, err := loading.LoadPackages(ctx)
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

		targets, err := model.TargetMapFromPackages(packages)
		if err != nil {
			logger.Fatalf("could not create target map: %v", err)
		}

		graph, err := analysis.BuildGraph(targets)
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
			// Select transitive dependencies of the selected targets
			for _, target := range graph.GetSelectedVertices() {
				for _, ancestor := range graph.GetAncestors(target) {
					ancestor.IsSelected = true
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
	// Deterministic ordering of vertices.
	vertices := graph.GetVertices().SelectedTargetsAlphabetically()
	nodeMap := make(map[string]*flowchart.Node)
	for _, vertex := range vertices {
		node := chart.AddNode(vertex.Label.String())
		node.Style = &flowchart.NodeStyle{
			Fill:        "#E3F2FD", // light-blue-50
			Stroke:      "#1E88E5", // blue-600
			StrokeWidth: 2,
		}
		nodeMap[vertex.Label.String()] = node

		// Add inputs as separate nodes if requested.
		if graphOptions.mermaidInputsAsNodes {
			// Keep input nodes in stable order as well.
			inputs := make([]string, len(vertex.UnresolvedInputs))
			copy(inputs, vertex.UnresolvedInputs)
			sort.Strings(inputs)

			for _, input := range inputs {
				inputNode := chart.AddNode(input)
				inputNode.Style = &flowchart.NodeStyle{
					Fill:        "#FFF8E1", // amber-50
					Stroke:      "#FB8C00", // orange-600
					StrokeWidth: 2,
					StrokeDash:  "6 3", // dashed border emphasises "just an input"
				}
				link := chart.AddLink(inputNode, node)
				link.Shape = flowchart.LinkShapeDotted
			}
		}
	}

	// Add Edges:
	out := graph.GetOutEdges()

	// Sort the map keys.
	var fromKeys []label.TargetLabel
	for k := range out {
		fromKeys = append(fromKeys, k)
	}
	sort.Slice(fromKeys, func(i, j int) bool {
		return fromKeys[i].String() < fromKeys[j].String()
	})

	for _, from := range fromKeys {
		toList := out[from]
		// Sort each destination slice.
		sort.Slice(toList, func(i, j int) bool {
			return toList[i].Label.String() < toList[j].Label.String()
		})

		for _, to := range toList {
			chart.AddLink(nodeMap[from.String()], nodeMap[to.Label.String()])
		}
	}

	fmt.Println(chart.String())
}

func printTree(graph *dag.DirectedTargetGraph) {
	// Gather all vertices that have no dependants – they are the roots.
	var roots []*model.Target
	for _, v := range graph.GetVertices() {
		if len(graph.GetDependants(v)) == 0 {
			roots = append(roots, v)
		}
	}

	// Deterministic order of roots.
	sort.Slice(roots, func(i, j int) bool {
		return roots[i].Label.String() < roots[j].Label.String()
	})

	for _, root := range roots {
		rootTree := buildTree(root, graph, 0)
		fmt.Println(rootTree)
	}
}

// buildTree converts a vertex and all of its (transitive) dependencies into a
// Lipgloss tree.
func buildTree(
	target *model.Target,
	graph *dag.DirectedTargetGraph,
	level int,
) *tree.Tree {
	targetLabel := target.Label.String()
	t := tree.New().Root(targetLabel)

	if level == 0 {
		enumeratorStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("63")).MarginRight(1)
		rootStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("35"))
		itemStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("212"))
		t.RootStyle(rootStyle).ItemStyle(itemStyle).EnumeratorStyle(enumeratorStyle).Enumerator(tree.RoundedEnumerator)
	}

	// Deterministic output – sort the direct dependencies alphabetically.
	deps := graph.GetDependencies(target)
	sort.Slice(deps, func(i, j int) bool {
		return deps[i].Label.String() < deps[j].Label.String()
	})

	for _, dep := range deps {
		depLabel := dep.Label.String()

		if len(graph.GetDependencies(dep)) > 0 {
			t.Child(buildTree(dep, graph, level+1))
		} else {
			t.Child(depLabel)
		}
	}

	return t
}
