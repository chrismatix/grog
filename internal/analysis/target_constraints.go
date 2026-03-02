package analysis

import (
	"fmt"
	"path"
	"path/filepath"
	"strings"

	"github.com/fatih/color"
	"grog/internal/config"
	"grog/internal/console"
	"grog/internal/label"
	"grog/internal/model"
)

/*
We need to check the following four constraints for the paths defined by each target:
1. all inputs must be relative to the package path
2. all outputs point to files within the repository
TODO 3. warn if a target's inputs intersect with another target's outputs without them explicitly depending on each other
TODO 4. error if a target's inputs intersect with its own outputs?

TODO: We don't yet check that a parent package does not include inputs from children. Should we?
*/

// CheckTargetConstraints checks that the paths defined by each target are valid
// logs any warnings on the way
func CheckTargetConstraints(logger *console.Logger, nodeMap model.BuildNodeMap) (errs []error) {
	// iterate over targets in alphabetical order for consistent logging
	for _, node := range nodeMap.NodesAlphabetically() {
		target, ok := node.(*model.Target)
		if !ok {
			continue
		}

		inputRelativeError := checkInputPathsRelative(target)
		errs = append(errs, inputRelativeError...)

		outputsError := checkOutputsAreWithinRepository(target)
		errs = append(errs, outputsError...)

		if target.IsTest() && target.Command == "" {
			errs = append(errs, fmt.Errorf("target %s is a test target but has no command", target.Label))
		}

		if len(target.Inputs) == 0 && len(target.Dependencies) == 0 && len(target.OutputChecks) == 0 {
			logger.Warnf("target %s has no inputs, dependencies or output checks causing it to run only once", target.Label)
		}
	}

	dependencyConstraintErrors := checkDependencyConstraints(nodeMap)
	errs = append(errs, dependencyConstraintErrors...)

	return
}

func checkDependencyConstraints(nodeMap model.BuildNodeMap) (errs []error) {
	for _, node := range nodeMap.NodesAlphabetically() {
		target, isTarget := node.(*model.Target)
		if !isTarget {
			continue
		}

		for _, dependencyLabel := range target.Dependencies {
			dependencyTarget := resolveDependencyTarget(nodeMap, dependencyLabel)
			if dependencyTarget == nil {
				continue
			}

			if dependencyTarget.IsTest() && !target.IsTest() {
				errs = append(errs, fmt.Errorf("%s depends on %s which is a test target",
					target.Label,
					dependencyTarget.Label,
				))
				continue
			}

			if dependencyTarget.IsTestOnly() && !target.IsTestOnly() && !target.IsTest() {
				errs = append(errs, fmt.Errorf("%s depends on %s which is tagged %q",
					target.Label,
					dependencyTarget.Label,
					model.TagTestOnly,
				))
			}
		}
	}

	return
}

func resolveDependencyTarget(
	nodeMap model.BuildNodeMap,
	dependencyLabel label.TargetLabel,
) *model.Target {
	currentDependencyLabel := dependencyLabel
	visitedDependencyLabels := make(map[label.TargetLabel]struct{})
	for {
		if _, alreadyVisited := visitedDependencyLabels[currentDependencyLabel]; alreadyVisited {
			return nil
		}
		visitedDependencyLabels[currentDependencyLabel] = struct{}{}

		dependencyNode, hasDependencyNode := nodeMap[currentDependencyLabel]
		if !hasDependencyNode {
			return nil
		}

		dependencyTarget, isTarget := dependencyNode.(*model.Target)
		if isTarget {
			return dependencyTarget
		}

		dependencyAlias, isAlias := dependencyNode.(*model.Alias)
		if !isAlias {
			return nil
		}

		currentDependencyLabel = dependencyAlias.Actual
	}
}

// checkInputPathsRelative checks that all inputs are relative to the package path
// and do not point outside the package
func checkInputPathsRelative(target *model.Target) (errs []error) {
	for _, input := range target.Inputs {
		if path.IsAbs(input) {
			errs = append(errs, fmt.Errorf(
				"input %s for target %s is not relative",
				input,
				target.Label))
			continue
		}

		triesToEscapePackage := pathTriesToEscape(input)
		if triesToEscapePackage {
			errs = append(errs, fmt.Errorf(
				"input %s for target %s points outside the package. Use %s to declare dependencies between targets",
				input,
				target.Label,
				color.New(color.Bold).Sprintf("deps")))
		}
	}

	return
}

const relativePrefix = ".." + string(filepath.Separator)

func pathTriesToEscape(relPath string) bool {
	cleanedPath := filepath.Clean(relPath)
	return strings.HasPrefix(cleanedPath, relativePrefix) || cleanedPath == ".."
}

func checkOutputsAreWithinRepository(target *model.Target) (errs []error) {
	workspaceRoot := config.Global.WorkspaceRoot

	for _, output := range target.FileOutputs() {
		if path.IsAbs(output) {
			errs = append(errs, fmt.Errorf(
				"output %s for target %s is not relative",
				output,
				target.Label))
			continue
		}

		check, err := isWithinWorkspace(workspaceRoot, target.Label.Package, output)
		if err != nil {
			errs = append(errs, fmt.Errorf(
				"could not check output %s for target %s: %w",
				output,
				target.Label,
				err,
			))
			continue
		}
		if !check {
			errs = append(errs, fmt.Errorf(
				"output %s for target %s points outside the repository",
				output,
				target.Label,
			))
		}
	}

	return
}

// isWithinWorkspace checks whether the resolved path (when joined with a starting directory)
// remains within the workspace root. The relative path may start with "..", but once resolved,
// it must still be inside the workspace.
func isWithinWorkspace(absWorkspace, packagePath, relPath string) (bool, error) {
	absOutput, err := filepath.Abs(filepath.Join(absWorkspace, packagePath, relPath))
	if err != nil {
		return false, err
	}

	// Compute the relative path from the workspace to the target.
	// If absTarget is within absWorkspace, the computed relative path will NOT start with ".."
	// (even if the user-supplied relative path started with "..").
	rel, err := filepath.Rel(absWorkspace, absOutput)
	if err != nil {
		return false, err
	}

	// Check if the path escapes the workspace.
	if pathTriesToEscape(rel) {
		return false, nil
	}
	return true, nil
}
