package cmds

import (
	"fmt"
	"path/filepath"
	"sort"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/tree"
	"github.com/spf13/cobra"

	"grog/internal/analysis"
	"grog/internal/config"
	"grog/internal/console"
	"grog/internal/dag"
	"grog/internal/label"
	"grog/internal/loading"
	"grog/internal/model"
)

var explainChangesOptions struct {
	since      string
	showFiles  bool
	filesFirst bool
}

var ExplainChangesCmd = &cobra.Command{
	Use:   "explain-changes",
	Short: "Renders the chain of targets affected by changes since a revision as a tree.",
	Long: `Shows, as a tree, how file changes since a given Git ref or Jujutsu revision propagate through the dependency graph.

By default the tree is rooted on the leaf consumers of the change — i.e. the top-level targets
(binaries, tests, etc.) that ultimately depend on the changed code — and walks back through their
dependencies to the directly-affected targets, with the changed input files attached as leaves.
This surfaces the actionable answer (which top-level targets need to be rebuilt / retested) first
and uses the chain underneath as the evidence.

This is the human-readable counterpart to ` + "`grog changes`" + `: same query, tree view. Use
` + "`grog changes`" + ` when you want a flat, pipeable list of target labels.

Use --files-first to flip the tree the other way: root on the changed files and walk downstream
through the directly-affected targets to their transitive dependents.`,
	Example: `  grog explain-changes --since=HEAD~1                      # Explain impact of the last commit
  grog explain-changes --since=main                        # Explain impact since the main branch
  grog explain-changes --since=main --show-files=false     # Drop the changed-file leaves
  grog explain-changes --since=main --files-first          # Flip to a file-rooted, downstream tree`,
	Args: cobra.MaximumNArgs(0),
	Run: func(cmd *cobra.Command, args []string) {
		ctx, logger := console.SetupCommand()

		if explainChangesOptions.since == "" {
			logger.Fatalf("--since flag is required")
		}

		changedFiles, err := getChangedFiles(explainChangesOptions.since)
		if err != nil {
			logger.Fatalf("Failed to get changed files: %v", err)
		}

		if len(changedFiles) == 0 {
			logger.Debug("No files changed")
			return
		}
		logger.Debugf("Changed files: %v", changedFiles)

		packages, err := loading.LoadAllPackages(ctx)
		if err != nil {
			logger.Fatalf("could not load packages: %v", err)
		}

		nodes, err := model.BuildNodeMapFromPackages(packages)
		if err != nil {
			logger.Fatalf("could not create target map: %v", err)
		}

		graph, err := analysis.BuildGraph(nodes)
		if err != nil {
			logger.Fatalf("could not build graph: %v", err)
		}

		// Build the file -> directly-affected targets map. Same matching logic as
		// `grog changes` (input files + package source files), but we preserve the
		// per-file association so the tree can root on files.
		fileToTargets := make(map[string][]*model.Target)
		seenPerFile := make(map[string]map[label.TargetLabel]bool)
		add := func(file string, target *model.Target) {
			if seenPerFile[file] == nil {
				seenPerFile[file] = make(map[label.TargetLabel]bool)
			}
			if seenPerFile[file][target.Label] {
				return
			}
			seenPerFile[file][target.Label] = true
			fileToTargets[file] = append(fileToTargets[file], target)
		}

		for _, target := range nodes.GetTargets() {
			for _, inputFile := range target.Inputs {
				absInputPath := config.GetPathAbsoluteToWorkspaceRoot(filepath.Join(
					target.Label.Package,
					inputFile,
				))
				if containsFile(changedFiles, absInputPath) {
					add(absInputPath, target)
				}
			}
		}
		for _, pkg := range packages {
			for _, target := range pkg.Targets {
				if containsFile(changedFiles, target.SourceFilePath) {
					add(target.SourceFilePath, target)
				}
			}
		}

		if len(fileToTargets) == 0 {
			logger.Debug("No targets affected by the changed files")
			return
		}

		switch {
		case explainChangesOptions.filesFirst && explainChangesOptions.showFiles:
			printFileRootedTree(fileToTargets, graph)
		case explainChangesOptions.filesFirst:
			printTargetRootedTree(fileToTargets, graph)
		default:
			affected, targetToFiles := computeAffectedSet(fileToTargets, graph)
			printConsumerRootedTree(affected, targetToFiles, graph, explainChangesOptions.showFiles)
		}
	},
}

// printFileRootedTree prints one tree per changed file: file -> affected
// targets -> transitive dependents.
func printFileRootedTree(fileToTargets map[string][]*model.Target, graph *dag.DirectedTargetGraph) {
	files := make([]string, 0, len(fileToTargets))
	for f := range fileToTargets {
		files = append(files, f)
	}
	sort.Strings(files)

	for _, f := range files {
		root := tree.New().Root(displayPath(f))
		applyRootStyle(root, downEnumerator)

		targets := fileToTargets[f]
		sort.Slice(targets, func(i, j int) bool {
			return targets[i].Label.String() < targets[j].Label.String()
		})
		for _, t := range targets {
			root.Child(buildDependentTree(t, graph))
		}
		fmt.Println(root)
	}
}

// printTargetRootedTree collapses the file layer and prints one tree per
// directly-affected target with its transitive dependent chain.
func printTargetRootedTree(fileToTargets map[string][]*model.Target, graph *dag.DirectedTargetGraph) {
	seen := make(map[label.TargetLabel]bool)
	var roots []*model.Target
	for _, targets := range fileToTargets {
		for _, t := range targets {
			if seen[t.Label] {
				continue
			}
			seen[t.Label] = true
			roots = append(roots, t)
		}
	}
	sort.Slice(roots, func(i, j int) bool {
		return roots[i].Label.String() < roots[j].Label.String()
	})

	for _, t := range roots {
		subtree := buildDependentTree(t, graph)
		applyRootStyle(subtree, downEnumerator)
		fmt.Println(subtree)
	}
}

// buildDependentTree builds a Lipgloss tree rooted at `node`, recursing
// downstream through `GetDependants`. Mirrors graph.go's buildTree but follows
// the dependency direction in reverse. No visited-set: the graph is acyclic
// (validated by BuildGraph), so we render diamond subtrees verbatim under each
// path that reaches them, matching the behavior of `grog graph`.
func buildDependentTree(node model.BuildNode, graph *dag.DirectedTargetGraph) *tree.Tree {
	t := tree.New().Root(node.GetLabel().String())

	deps := graph.GetDependants(node)
	sort.Slice(deps, func(i, j int) bool {
		return deps[i].GetLabel().String() < deps[j].GetLabel().String()
	})

	for _, dep := range deps {
		if len(graph.GetDependants(dep)) > 0 {
			t.Child(buildDependentTree(dep, graph))
		} else {
			t.Child(dep.GetLabel().String())
		}
	}
	return t
}

// computeAffectedSet returns the full set of nodes touched by the change
// (directly-affected targets plus all their transitive dependents) along with a
// reverse target -> changed-files index. The returned set keys on label so root
// detection can ask "do any dependants of this node also live in the set?"
// while keeping the BuildNode handy for traversal.
func computeAffectedSet(
	fileToTargets map[string][]*model.Target,
	graph *dag.DirectedTargetGraph,
) (map[label.TargetLabel]model.BuildNode, map[label.TargetLabel][]string) {
	affected := make(map[label.TargetLabel]model.BuildNode)
	targetToFiles := make(map[label.TargetLabel][]string)

	for file, targets := range fileToTargets {
		for _, t := range targets {
			affected[t.Label] = t
			targetToFiles[t.Label] = append(targetToFiles[t.Label], file)
			for _, d := range graph.GetDescendants(t) {
				affected[d.GetLabel()] = d
			}
		}
	}

	// Deterministic file ordering per target.
	for lbl := range targetToFiles {
		sort.Strings(targetToFiles[lbl])
	}
	return affected, targetToFiles
}

// printConsumerRootedTree is the default view. It roots on the leaf consumers
// of the change (nodes in the affected set whose dependants are all outside the
// set) and walks back through dependencies, stopping naturally at the
// directly-affected targets. When showFiles is true, each directly-affected
// target has its changed input files attached as leaves under it.
func printConsumerRootedTree(
	affected map[label.TargetLabel]model.BuildNode,
	targetToFiles map[label.TargetLabel][]string,
	graph *dag.DirectedTargetGraph,
	showFiles bool,
) {
	var roots []model.BuildNode
	for _, node := range affected {
		isRoot := true
		for _, dep := range graph.GetDependants(node) {
			if _, in := affected[dep.GetLabel()]; in {
				isRoot = false
				break
			}
		}
		if isRoot {
			roots = append(roots, node)
		}
	}
	sort.Slice(roots, func(i, j int) bool {
		return roots[i].GetLabel().String() < roots[j].GetLabel().String()
	})

	for _, r := range roots {
		t := buildUpstreamTree(r, graph, affected, targetToFiles, showFiles)
		applyRootStyle(t, upEnumerator)
		fmt.Println(t)
	}
}

// buildUpstreamTree builds a Lipgloss tree rooted at `node`, recursing upstream
// through `GetDependencies` but only following edges into nodes that are in the
// affected set. Optionally attaches changed input files as leaf children for
// nodes that are directly-affected. As with buildDependentTree, no visited-set
// is kept: diamond subtrees repeat under each path that reaches them.
func buildUpstreamTree(
	node model.BuildNode,
	graph *dag.DirectedTargetGraph,
	affected map[label.TargetLabel]model.BuildNode,
	targetToFiles map[label.TargetLabel][]string,
	showFiles bool,
) *tree.Tree {
	t := tree.New().Root(node.GetLabel().String())

	if showFiles {
		for _, f := range targetToFiles[node.GetLabel()] {
			t.Child(displayPath(f))
		}
	}

	deps := graph.GetDependencies(node)
	sort.Slice(deps, func(i, j int) bool {
		return deps[i].GetLabel().String() < deps[j].GetLabel().String()
	})
	for _, dep := range deps {
		if _, in := affected[dep.GetLabel()]; !in {
			continue
		}
		t.Child(buildUpstreamTree(dep, graph, affected, targetToFiles, showFiles))
	}
	return t
}

// upEnumerator and downEnumerator replace the dash in the standard rounded
// connector with an arrow pointing in the direction of change propagation:
//
//   - up   (╰─↑ / ├─↑): consumer-rooted view — change flows UP from each child
//     line to its parent (cause is below, effect is above).
//   - down (╰─↓ / ├─↓): files-first view — change flows DOWN from each parent
//     to its children (cause is above, effect is below).
//
// Both keep the standard 3-char width so they align with lipgloss's default
// 3-char indenter without further configuration.
func upEnumerator(c tree.Children, i int) string {
	if i == c.Length()-1 {
		return "╰─↑"
	}
	return "├─↑"
}

func downEnumerator(c tree.Children, i int) string {
	if i == c.Length()-1 {
		return "╰─↓"
	}
	return "├─↓"
}

// applyRootStyle matches the coloring used by `grog graph` at its top level so
// the two tree views feel consistent, and installs the supplied enumerator —
// lipgloss propagates the top-level enumerator to nested sub-trees.
func applyRootStyle(t *tree.Tree, enum tree.Enumerator) {
	enumeratorStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("63")).MarginRight(1)
	rootStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("35"))
	itemStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("212"))
	t.RootStyle(rootStyle).
		ItemStyle(itemStyle).
		EnumeratorStyle(enumeratorStyle).
		Enumerator(enum)
}

// displayPath renders an absolute file path relative to the workspace root for
// readability. Falls back to the absolute path if a relative one can't be
// derived.
func displayPath(absPath string) string {
	rel, err := filepath.Rel(config.Global.WorkspaceRoot, absPath)
	if err != nil {
		return absPath
	}
	return rel
}

func AddExplainChangesCmd(rootCmd *cobra.Command) {
	ExplainChangesCmd.Flags().StringVar(
		&explainChangesOptions.since,
		"since",
		"",
		"Git ref or Jujutsu revision to compare against")

	ExplainChangesCmd.Flags().BoolVar(
		&explainChangesOptions.showFiles,
		"show-files",
		true,
		"Include the changed input files in the tree. By default files are leaves under the directly-affected targets; with --files-first they are tree roots. Use --show-files=false to omit them.")

	ExplainChangesCmd.Flags().BoolVar(
		&explainChangesOptions.filesFirst,
		"files-first",
		false,
		"Flip the tree to root on the changed files and walk downstream through the directly-affected targets to their transitive dependents.")

	rootCmd.AddCommand(ExplainChangesCmd)
}
