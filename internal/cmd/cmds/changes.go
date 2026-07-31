package cmds

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"grog/internal/cmd/flagtypes"
	"grog/internal/config"
	"grog/internal/console"
	"grog/internal/dag"
	"grog/internal/label"
	"grog/internal/loading"
	"grog/internal/model"
	"grog/internal/selection"

	"github.com/spf13/cobra"
)

var changesOptions = struct {
	since      string
	dependents *flagtypes.Enum
	targetType *flagtypes.Enum
}{
	dependents: flagtypes.NewEnum("none", "transitive"),
	targetType: flagtypes.NewEnum("all", "test", "no_test", "bin_output"),
}

var ChangesCmd = &cobra.Command{
	Use:   "changes",
	Short: "Lists targets whose inputs have been modified since a given commit.",
	Long: `Identifies targets that need to be rebuilt due to changes in their input files since a specified Git commit or Jujutsu revision.
Can optionally include transitive dependents of changed targets to find all affected targets.`,
	Example: `  grog changes --since=HEAD~1                      # Show targets changed in the last commit
  grog changes --since=main --dependents=transitive  # Show targets changed since main branch, including dependents
  grog changes --since=v1.0.0 --target-type=test     # Show only test targets changed since Git tag v1.0.0`,
	Args: cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		ctx, logger := console.SetupCommand()

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

		graph := loading.MustLoadGraphForQuery(ctx, logger)
		affectedTargets := findTargetsAffectedByFiles(
			graph,
			changedFiles,
			changesOptions.dependents.Value == "transitive",
		)
		affectedNodes := make([]model.BuildNode, 0, len(affectedTargets))
		for _, target := range affectedTargets {
			affectedNodes = append(affectedNodes, target)
		}

		targetTypeFilter, err := selection.StringToTargetTypeSelection(changesOptions.targetType.Value)
		if err != nil {
			logger.Fatalf(err.Error())
		}
		selector := selection.New(nil, config.Global.Tags, config.Global.ExcludeTags, targetTypeFilter)

		model.PrintSortedLabels(selector.FilterNodes(affectedNodes))
	},
}

func findTargetsAffectedByFiles(
	graph *dag.DirectedTargetGraph,
	changedFiles []string,
	includeDependents bool,
) []*model.Target {
	affectedTargets := make(map[label.TargetLabel]*model.Target)

	for _, node := range graph.GetNodes() {
		target, ok := node.(*model.Target)
		if !ok {
			continue
		}

		if containsFile(changedFiles, target.SourceFilePath) {
			affectedTargets[target.Label] = target
			continue
		}

		for _, inputFile := range target.Inputs {
			absoluteInputPath := config.GetPathAbsoluteToWorkspaceRoot(filepath.Join(
				target.Label.Package,
				inputFile,
			))
			if containsFile(changedFiles, absoluteInputPath) {
				affectedTargets[target.Label] = target
				break
			}
		}
	}

	if includeDependents {
		directlyAffectedTargets := make([]*model.Target, 0, len(affectedTargets))
		for _, target := range affectedTargets {
			directlyAffectedTargets = append(directlyAffectedTargets, target)
		}
		for _, target := range directlyAffectedTargets {
			for _, descendant := range graph.GetDescendants(target) {
				if descendantTarget, ok := descendant.(*model.Target); ok {
					affectedTargets[descendantTarget.Label] = descendantTarget
				}
			}
		}
	}

	targets := make([]*model.Target, 0, len(affectedTargets))
	for _, target := range affectedTargets {
		targets = append(targets, target)
	}
	return targets
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
	canonicalFile := canonicalPath(file)
	for _, candidate := range files {
		if canonicalPath(candidate) == canonicalFile {
			return true
		}
	}
	return false
}

func canonicalPath(path string) string {
	canonical, err := filepath.EvalSymlinks(path)
	if err == nil {
		return canonical
	}
	return filepath.Clean(path)
}

func AddChangesCmd(rootCmd *cobra.Command) {
	flags := ChangesCmd.Flags()

	flags.StringVar(
		&changesOptions.since,
		"since",
		"",
		"Git ref or Jujutsu revision to compare against")

	flags.Var(
		changesOptions.dependents,
		"dependents",
		"Whether to include dependents of changed targets (none or transitive)")

	flags.Var(
		changesOptions.targetType,
		"target-type",
		"Filter targets by type (all, test, no_test, bin_output)")

	if err := ChangesCmd.MarkFlagRequired("since"); err != nil {
		panic(err)
	}
	rootCmd.AddCommand(ChangesCmd)
}
