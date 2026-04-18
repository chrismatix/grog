package cmds

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"grog/internal/analysis"
	"grog/internal/config"
	"grog/internal/console"
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
	Long: `Identifies targets that need to be rebuilt due to changes in their input files since a specified Git commit or Jujutsu revision.
Can optionally include transitive dependents of changed targets to find all affected targets.`,
	Example: `  grog changes --since=HEAD~1                      # Show targets changed in the last commit
  grog changes --since=main --dependents=transitive  # Show targets changed since main branch, including dependents
  grog changes --since=v1.0.0 --target-type=test     # Show only test targets changed since Git tag v1.0.0`,
	Args: cobra.MaximumNArgs(0),
	Run: func(cmd *cobra.Command, args []string) {
		ctx, logger := console.SetupCommand()

		if changesOptions.since == "" {
			logger.Fatalf("--since flag is required")
		}

		if changesOptions.dependents != "none" && changesOptions.dependents != "transitive" {
			logger.Fatalf("--dependents must be either 'none' or 'transitive'")
		}

		// Get changed files using git or jj
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

// getChangedFiles returns a list of files that have changed since the given revision
func getChangedFiles(revision string) ([]string, error) {
	gitRoot, err := getGitRoot()
	if err != nil {
		return nil, err
	}

	outputs := [][]byte{}

	if vcsIsJJ(gitRoot) {
		output, err := getChangedFilesForJujutsuRevision(gitRoot, revision)
		if err != nil {
			return nil, err
		}
		outputs = append(outputs, output)
	} else {
		output, err := getChangedFilesForGitRevision(gitRoot, revision)
		if err != nil {
			return nil, err
		}
		outputs = append(outputs, output)

		dirtyOutputs, err := getDirtyChangedFiles(gitRoot)
		if err != nil {
			return nil, err
		}
		outputs = append(outputs, dirtyOutputs...)
	}

	uniqueFiles := make(map[string]bool)
	for _, output := range outputs {
		for file := range strings.SplitSeq(string(output), "\n") {
			if file != "" {
				uniqueFiles[file] = true
			}
		}
	}

	var files []string
	for file := range uniqueFiles {
		// Get the absolute path of the file
		absolutePath := filepath.Join(gitRoot, file)
		files = append(files, absolutePath)
	}

	return files, nil
}

func getGitRoot() (string, error) {
	gitRootCommand := exec.Command("git", "rev-parse", "--show-toplevel")
	gitRootOutput, err := gitRootCommand.Output()
	if err != nil {
		return "", err
	}

	return strings.TrimSpace(string(gitRootOutput)), nil
}

func getChangedFilesForGitRevision(gitRoot string, revision string) ([]byte, error) {
	// Run a tree-only diff to get changed files. Disabling rename detection keeps
	// this path-based query compatible with blobless partial clones.
	gitDiffCommand := exec.Command(
		"git",
		"diff-tree",
		"--name-only",
		"-r",
		"--no-commit-id",
		"--no-renames",
		revision,
		"HEAD",
	)
	gitDiffCommand.Dir = gitRoot
	return gitDiffCommand.Output()
}

func getChangedFilesForJujutsuRevision(gitRoot string, revision string) ([]byte, error) {
	jujutsuDiffCommand := exec.Command(
		"jj",
		"--no-pager",
		"--color",
		"never",
		"diff",
		"--name-only",
		"--from",
		revision,
		"--to",
		"@",
	)
	jujutsuDiffCommand.Dir = gitRoot
	return jujutsuDiffCommand.Output()
}

func getDirtyChangedFiles(gitRoot string) ([][]byte, error) {
	commands := []*exec.Cmd{
		exec.Command("git", "diff-index", "--cached", "--name-only", "--no-renames", "HEAD"),
		exec.Command("git", "diff-files", "--name-only", "--no-renames"),
		exec.Command("git", "ls-files", "--others", "--exclude-standard"),
	}

	var outputs [][]byte
	for _, command := range commands {
		command.Dir = gitRoot

		output, err := command.Output()
		if err != nil {
			return nil, err
		}
		outputs = append(outputs, output)
	}

	return outputs, nil
}

func vcsIsJJ(gitRoot string) bool {
	jujutsuDirectoryPath := filepath.Join(gitRoot, ".jj")
	fileInfo, err := os.Stat(jujutsuDirectoryPath)
	if err != nil {
		return false
	}
	return fileInfo.IsDir()
}

// containsFile checks if the list of files contains the given file
func containsFile(files []string, file string) bool {
	return slices.Contains(files, file)
}

func AddChangesCmd(rootCmd *cobra.Command) {
	ChangesCmd.Flags().StringVar(
		&changesOptions.since,
		"since",
		"",
		"Git ref or Jujutsu revision to compare against")

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
