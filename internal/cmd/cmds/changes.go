package cmds

import (
	"grog/internal/analysis"
	"grog/internal/console"
	"os/exec"
	"path/filepath"
	"strings"

	"grog/internal/config"
	"grog/internal/label"
	"grog/internal/loading"
	"grog/internal/model"
	"grog/internal/selection"

	"github.com/spf13/cobra"
)

var changesOptions struct {
	since      string
	dependents string
	targetType string
}

var ChangesCmd = &cobra.Command{
	Use:   "changes",
	Short: "Lists targets whose inputs have been modified since a given commit.",
	Long: `Identifies targets that need to be rebuilt due to changes in their input files since a specified git commit.
Can optionally include transitive dependents of changed targets to find all affected targets.`,
	Example: `  grog changes --since=HEAD~1                      # Show targets changed in the last commit
  grog changes --since=main --dependents=transitive  # Show targets changed since main branch, including dependents
  grog changes --since=gen.0.0 --target-type=test     # Show only test targets changed since gen.0.0`,
	Args: cobra.MaximumNArgs(0),
	Run: func(cmd *cobra.Command, args []string) {
		ctx, logger := console.SetupCommand()

		if changesOptions.since == "" {
			logger.Fatalf("--since flag is required")
		}

		if changesOptions.dependents != "none" && changesOptions.dependents != "transitive" {
			logger.Fatalf("--dependents must be either 'none' or 'transitive'")
		}

		// Get changed files using git
		changedFiles, err := getChangedFiles(changesOptions.since)
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
			logger.Fatalf(
				"could not load packages: %v",
				err)
		}

		nodes, err := model.BuildNodeMapFromPackages(packages)
		if err != nil {
			logger.Fatalf("could not create target map: %v", err)
		}

		graph, err := analysis.BuildGraph(nodes)
		if err != nil {
			logger.Fatalf("could not build graph: %v", err)
		}

		// Find nodes that own the changed files
		var matchingTargets []*model.Target
		for _, target := range nodes.GetTargets() {
			for _, inputFile := range target.Inputs {
				// Get the absolute path of the input file
				absInputPath := config.GetPathAbsoluteToWorkspaceRoot(filepath.Join(
					target.Label.Package,
					inputFile,
				))

				if containsFile(changedFiles, absInputPath) {
					matchingTargets = append(matchingTargets, target)
					break // Found a match, no need to check other inputs
				}
			}
		}

		// Check package definitions
		for _, pkg := range packages {
			for _, target := range pkg.Targets {
				if containsFile(changedFiles, target.SourceFilePath) {
					// Add this target if the package source file changed
					matchingTargets = append(matchingTargets, target)
				}
			}
		}

		// Get dependents if requested
		var resultTargets []*model.Target
		if changesOptions.dependents == "transitive" {
			// Get all transitive dependents of the matching nodes
			for _, target := range matchingTargets {
				resultTargets = append(resultTargets, target)
				for _, descendant := range graph.GetDescendants(target) {
					if targetDescendant, ok := descendant.(*model.Target); ok {
						resultTargets = append(resultTargets, targetDescendant)
					}
				}
			}
		} else {
			resultTargets = matchingTargets
		}

		// Deduplicate nodes
		uniqueLabels := make(map[label.TargetLabel]bool)
		var deduplicatedTargets []model.BuildNode
		for _, target := range resultTargets {
			if !uniqueLabels[target.Label] {
				uniqueLabels[target.Label] = true
				deduplicatedTargets = append(deduplicatedTargets, target)
			}
		}

		targetTypeFilter, err := selection.StringToTargetTypeSelection(changesOptions.targetType)
		if err != nil {
			logger.Fatalf(err.Error())
		}
		selector := selection.New(nil, config.Global.Tags, config.Global.ExcludeTags, targetTypeFilter)

		model.PrintSortedLabels(selector.FilterNodes(deduplicatedTargets))
	},
}

// getChangedFiles returns a list of files that have changed since the given git ref
func getChangedFiles(gitRef string) ([]string, error) {
	// Run git diff to get changed files
	gitDiffCmd := exec.Command("git", "diff", "--name-only", gitRef)
	output, err := gitDiffCmd.Output()
	if err != nil {
		return nil, err
	}

	// get the git root
	gitRootCmd := exec.Command("git", "rev-parse", "--show-toplevel")
	gitRootOutput, err := gitRootCmd.Output()
	if err != nil {
		return nil, err
	}
	gitRoot := strings.TrimSpace(string(gitRootOutput))

	// Split the output into lines and filter out empty lines
	var files []string
	for _, file := range strings.Split(string(output), "\n") {
		if file != "" {
			// Get the absolute path of the file
			absPath := filepath.Join(gitRoot, file)
			files = append(files, absPath)
		}
	}

	return files, nil
}

// containsFile checks if the list of files contains the given file
func containsFile(files []string, file string) bool {
	for _, f := range files {
		if f == file {
			return true
		}
	}
	return false
}

func AddChangesCmd(rootCmd *cobra.Command) {
	ChangesCmd.Flags().StringVar(
		&changesOptions.since,
		"since",
		"",
		"Git ref (commit or branch) to compare against")

	ChangesCmd.Flags().StringVar(
		&changesOptions.dependents,
		"dependents",
		"none",
		"Whether to include dependents of changed targets (none or transitive)")

	ChangesCmd.Flags().StringVar(
		&changesOptions.targetType,
		"target-type",
		"all",
		"Filter targets by type (all, test, no_test, bin_output)")

	rootCmd.AddCommand(ChangesCmd)
}
