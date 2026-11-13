package cmds

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/chrismatix/grog/internal/analysis"
	"github.com/chrismatix/grog/internal/cmd/flagtypes"
	"github.com/chrismatix/grog/internal/config"
	"github.com/chrismatix/grog/internal/console"
	"github.com/chrismatix/grog/internal/label"
	"github.com/chrismatix/grog/internal/loading"
	"github.com/chrismatix/grog/internal/model"
	"github.com/chrismatix/grog/internal/resolvers"
	"github.com/chrismatix/grog/internal/selection"

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

		packages, err := loading.LoadAllPackages(ctx, dependencyInferrer())
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

		for _, pkg := range packages {
			for _, resolver := range pkg.DependencyResolvers {
				for target := range resolverChanges(ctx, nodes, resolver, changedFiles, changesOptions.since) {
					matchingTargets = append(matchingTargets, target)
				}
			}
		}

		// Get dependents if requested
		var resultTargets []*model.Target
		if changesOptions.dependents.Value == "transitive" {
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

		targetTypeFilter, err := selection.StringToTargetTypeSelection(changesOptions.targetType.Value)
		if err != nil {
			logger.Fatalf(err.Error())
		}
		selector := selection.New(nil, config.Global.Tags, config.Global.ExcludeTags, targetTypeFilter)

		model.PrintSortedLabels(selector.FilterNodes(deduplicatedTargets))
	},
}

// getChangedFiles returns a list of files that have changed since the given revision.
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

// containsFile checks if the list of files contains the given file.
func containsFile(files []string, file string) bool {
	return slices.Contains(files, file)
}

func resolverInputChanged(resolver *model.DependencyResolver, changedFiles []string) bool {
	for _, inputFile := range resolver.Inputs {
		if containsFile(changedFiles, config.GetPathAbsoluteToWorkspaceRoot(filepath.Join(resolver.Label.Package, inputFile))) {
			return true
		}
	}
	return false
}

// resolverTargets lists the targets registered with, or synthesized by, a resolver.
func resolverTargets(nodes model.BuildNodeMap, resolverLabel label.TargetLabel) []*model.Target {
	var targets []*model.Target
	for _, target := range nodes.GetTargets() {
		if slices.Contains(target.DependencyResolvers, resolverLabel) {
			targets = append(targets, target)
		}
	}
	return targets
}

// comparableBuiltins fingerprint external dependencies. A lock bump changes no
// report of the other builtins, so they keep the blanket rule until they do.
var comparableBuiltins = []string{"builtin::go", "builtin::uv"}

// resolverChanges maps each target a changed resolver input affects to the
// reason. A comparable builtin is resolved at both revisions and only packages
// whose report differs count; any other resolver marks every target behind it.
func resolverChanges(changesContext context.Context, nodes model.BuildNodeMap, resolver *model.DependencyResolver, changedFiles []string, revision string) map[*model.Target]string {
	if !resolverInputChanged(resolver, changedFiles) {
		return nil
	}
	targets := resolverTargets(nodes, resolver.Label)
	reasons := make(map[*model.Target]string, len(targets))
	builtin := resolvers.Builtins[resolver.Command]
	isComparable := slices.Contains(comparableBuiltins, resolver.Command)
	var current, previous resolvers.Document
	var operationError error
	if isComparable {
		current, operationError = builtin.Resolve(changesContext, config.GetPathAbsoluteToWorkspaceRoot(resolver.Label.Package))
		if operationError == nil {
			previous, operationError = resolveAtRevision(changesContext, builtin, resolver, changedFiles, revision)
		}
	}
	if !isComparable || operationError != nil {
		if operationError != nil {
			console.GetLogger(changesContext).Debugf("resolver %s: could not compare against %s, so all its targets count as changed: %v", resolver.Label, revision, operationError)
		}
		for _, target := range targets {
			reasons[target] = "resolver input changed"
		}
		return reasons
	}

	registered := make(map[string]*model.Target, len(targets))
	for _, target := range targets {
		registered[target.Label.Package] = target
	}
	for _, packagePath := range slices.Concat(slices.Collect(maps.Keys(current.Packages)), slices.Collect(maps.Keys(previous.Packages))) {
		target := registered[path.Join(resolver.Label.Package, packagePath)]
		currentPackage, isReported := current.Packages[packagePath]
		previousPackage, wasReported := previous.Packages[packagePath]
		slices.Sort(currentPackage.Dependencies)
		slices.Sort(previousPackage.Dependencies)
		switch {
		case target == nil:
		case !wasReported:
			reasons[target] = "new package"
		case !isReported:
			reasons[target] = "no longer reported"
		case !slices.Equal(currentPackage.Dependencies, previousPackage.Dependencies):
			reasons[target] = "edges changed"
		case !slices.Equal(currentPackage.Inputs, previousPackage.Inputs) || !slices.Equal(currentPackage.ExcludeInputs, previousPackage.ExcludeInputs):
			reasons[target] = "inputs changed"
		case !maps.Equal(currentPackage.Fingerprint, previousPackage.Fingerprint):
			reasons[target] = "fingerprint changed"
		}
	}
	return reasons
}

// resolveAtRevision runs a builtin against its package as of a revision. Only
// the declared inputs and the changed files are laid out: unchanged inputs are
// copied from the working tree, changed files are read from version control.
func resolveAtRevision(resolveContext context.Context, builtin resolvers.Builtin, resolver *model.DependencyResolver, changedFiles []string, revision string) (resolvers.Document, error) {
	packageDirectory := config.GetPathAbsoluteToWorkspaceRoot(resolver.Label.Package)
	temporaryDirectory, operationError := os.MkdirTemp("", "grog-resolver-")
	if operationError != nil {
		return resolvers.Document{}, operationError
	}
	defer func() { _ = os.RemoveAll(temporaryDirectory) }()
	writeFile := func(absolutePath string, contents []byte) error {
		relativePath, operationError := filepath.Rel(packageDirectory, absolutePath)
		if operationError != nil {
			return operationError
		}
		destination := filepath.Join(temporaryDirectory, relativePath)
		if operationError := os.MkdirAll(filepath.Dir(destination), 0755); operationError != nil {
			return operationError
		}
		return os.WriteFile(destination, contents, 0644)
	}

	for _, input := range resolver.Inputs {
		inputPath := filepath.Join(packageDirectory, input)
		if containsFile(changedFiles, inputPath) {
			continue
		}
		contents, operationError := os.ReadFile(inputPath)
		if errors.Is(operationError, os.ErrNotExist) {
			continue
		}
		if operationError == nil {
			operationError = writeFile(inputPath, contents)
		}
		if operationError != nil {
			return resolvers.Document{}, operationError
		}
	}
	// Changed files beyond the inputs cover those deleted since the revision,
	// which were inputs then but are not listed now.
	var packageChangedFiles []string
	for _, changedFile := range changedFiles {
		if strings.HasPrefix(changedFile, packageDirectory+string(filepath.Separator)) {
			packageChangedFiles = append(packageChangedFiles, changedFile)
		}
	}
	contentsByFile, operationError := filesAtRevision(revision, packageChangedFiles)
	if operationError != nil {
		return resolvers.Document{}, operationError
	}
	for file, contents := range contentsByFile {
		if operationError := writeFile(file, contents); operationError != nil {
			return resolvers.Document{}, operationError
		}
	}
	return builtin.Resolve(resolveContext, temporaryDirectory)
}

// filesAtRevision reads absolute paths as of a revision in one git call and
// leaves out those that did not exist then. A Jujutsu revision is first
// mapped to its git commit, which requires a colocated repository.
func filesAtRevision(revision string, files []string) (map[string][]byte, error) {
	gitRoot, operationError := getGitRoot()
	if operationError != nil {
		return nil, operationError
	}
	if vcsIsJJ(gitRoot) {
		jujutsuLogCommand := exec.Command("jj", "--no-pager", "--color", "never", "log", "--no-graph", "-r", revision, "-T", "commit_id")
		jujutsuLogCommand.Dir = gitRoot
		commitOutput, operationError := jujutsuLogCommand.Output()
		if operationError != nil {
			return nil, fmt.Errorf("resolve jj revision %s: %w", revision, operationError)
		}
		revision = strings.TrimSpace(string(commitOutput))
	}
	var request strings.Builder
	for _, file := range files {
		relativePath, operationError := filepath.Rel(gitRoot, file)
		if operationError != nil {
			return nil, operationError
		}
		fmt.Fprintf(&request, "%s:%s\n", revision, filepath.ToSlash(relativePath))
	}
	catFileCommand := exec.Command("git", "cat-file", "--batch")
	catFileCommand.Dir = gitRoot
	catFileCommand.Stdin = strings.NewReader(request.String())
	output, operationError := catFileCommand.Output()
	if operationError != nil {
		return nil, fmt.Errorf("git cat-file: %w", operationError)
	}

	// Each answer is "<object> missing" or "<oid> <type> <size>", the
	// contents and a newline.
	reader := bufio.NewReader(bytes.NewReader(output))
	contentsByFile := make(map[string][]byte)
	for _, file := range files {
		header, operationError := reader.ReadString('\n')
		if operationError != nil {
			return nil, fmt.Errorf("git cat-file: %w", operationError)
		}
		if strings.HasSuffix(header, " missing\n") {
			continue
		}
		fields := strings.Fields(header)
		if len(fields) != 3 {
			return nil, fmt.Errorf("git cat-file: unexpected answer %q", header)
		}
		size, operationError := strconv.Atoi(fields[2])
		if operationError != nil {
			return nil, fmt.Errorf("git cat-file: unexpected answer %q", header)
		}
		contents := make([]byte, size+1)
		if _, operationError := io.ReadFull(reader, contents); operationError != nil {
			return nil, fmt.Errorf("git cat-file: %w", operationError)
		}
		if fields[1] == "blob" {
			contentsByFile[file] = contents[:size]
		}
	}
	return contentsByFile, nil
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
