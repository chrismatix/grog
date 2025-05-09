package cmds

import (
	"fmt"
	"grog/internal/analysis"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"grog/internal/config"
	"grog/internal/label"
	"grog/internal/loading"
	"grog/internal/model"
)

var changesOptions struct {
	since      string
	dependents string
}

var ChangesCmd = &cobra.Command{
	Use:   "changes",
	Short: "Lists targets whose inputs have been modified since a given commit.",
	Args:  cobra.MaximumNArgs(0),
	Run: func(cmd *cobra.Command, args []string) {
		ctx, logger := setupCommand()

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

		packages, err := loading.LoadPackages(ctx)
		if err != nil {
			logger.Fatalf(
				"could not load packages: %v",
				err)
		}

		targets, err := model.TargetMapFromPackages(packages)
		if err != nil {
			logger.Fatalf("could not create target map: %v", err)
		}

		graph, err := analysis.BuildGraph(targets)
		if err != nil {
			logger.Fatalf("could not build graph: %v", err)
		}

		// Find targets that own the changed files
		var matchingTargets []*model.Target
		for _, target := range targets {
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
			// Get the absolute path of the package definition
			absPkgPath := pkg.SourceFilePath
			fmt.Println(absPkgPath, changedFiles)
			if containsFile(changedFiles, absPkgPath) {
				fmt.Println("contains!")
				// add all targets within that package:
				for _, target := range pkg.Targets {
					matchingTargets = append(matchingTargets, target)
				}
			}
		}

		// Get dependents if requested
		var resultTargets []*model.Target
		if changesOptions.dependents == "transitive" {
			// Get all transitive dependents of the matching targets
			for _, target := range matchingTargets {
				resultTargets = append(resultTargets, target)
				for _, descendant := range graph.GetDescendants(target) {
					resultTargets = append(resultTargets, descendant)
				}
			}
		} else {
			resultTargets = matchingTargets
		}

		// Deduplicate targets
		uniqueLabels := make(map[label.TargetLabel]bool)
		var finalLabels []label.TargetLabel
		for _, target := range resultTargets {
			if !uniqueLabels[target.Label] {
				uniqueLabels[target.Label] = true
				finalLabels = append(finalLabels, target.Label)
			}
		}

		label.PrintSorted(finalLabels)
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

	rootCmd.AddCommand(ChangesCmd)
}
