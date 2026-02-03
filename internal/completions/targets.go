package completions

import (
	"fmt"
	"grog/internal/config"
	"grog/internal/console"
	"grog/internal/label"
	"grog/internal/loading"
	"grog/internal/model"
	"grog/internal/selection"
	os "os"
	"sort"
	"strings"

	"github.com/spf13/cobra"
)

func TargetPatternCompletion(command *cobra.Command, _ []string, toComplete string, targetType selection.TargetTypeSelection) ([]string, cobra.ShellCompDirective) {
	context, _ := console.SetupCommand()
	currentPackage, err := config.Global.GetCurrentPackage()
	debugToFile(fmt.Sprintf("err: %s\n", err))

	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp | cobra.ShellCompDirectiveError
	}

	// Completion input states.
	// Absolute: starts with "//", intended to resolve from workspace root.
	// Relative target: starts with ":", intended to resolve within current package only.
	// Partial prefix: a package path that is not fully qualified yet, e.g. "//pack".
	isAbsolute := strings.HasPrefix(toComplete, "//")
	isRelativeTarget := strings.HasPrefix(toComplete, ":")

	pattern := label.ParsePartialTargetPattern(currentPackage, toComplete)
	// Parsing state:
	// - Prefix(): resolved package prefix (may be partial for absolute patterns).
	// - Target(): target name fragment or directory fragment depending on input.
	// - IsPrefixPartial(): true when the user has typed an incomplete package path like "//pack".
	originalPrefix := pattern.Prefix()
	searchDirectory := originalPrefix
	targetPrefix := pattern.Target()
	directoryPrefix := targetPrefix
	isPrefixPartial := pattern.IsPrefixPartial()
	// When the prefix is partial, re-scope the search to the parent directory so we can
	// suggest siblings that share the same leading segment(s). Example:
	// - Input: "//pack"
	// - Prefix(): "pack"
	// - Search should happen at root, with "pack" used as the directory prefix filter.
	if isPrefixPartial && isAbsolute && !isRelativeTarget {
		baseDirectory, partialSegment := splitPartialPrefix(searchDirectory)
		searchDirectory = baseDirectory
		directoryPrefix = partialSegment
	}
	// Relative inputs without a package prefix should resolve from the current package.
	if searchDirectory == "" && !isAbsolute {
		searchDirectory = currentPackage
	}

	debugToFile(fmt.Sprintf("searchDir: %s\n", searchDirectory))
	debugToFile(fmt.Sprintf("target: %s\n", pattern.Target()))

	// Load packages from the search directory, which is either the current package,
	// the workspace root, or the parent directory for partial prefixes.
	absoluteSearchDirectory := config.GetPathAbsoluteToWorkspaceRoot(searchDirectory)
	packages, err := loading.LoadPackages(context, absoluteSearchDirectory)
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp | cobra.ShellCompDirectiveError
	}

	// For partial prefixes we also need packages for the original prefix so that we can
	// discover targets and child directories beneath the fully qualified package.
	packagesForOriginalPrefix := packages
	if isPrefixPartial && originalPrefix != "" && originalPrefix != searchDirectory {
		absoluteOriginalDir := config.GetPathAbsoluteToWorkspaceRoot(originalPrefix)
		packagesForOriginalPrefix, err = loading.LoadPackages(context, absoluteOriginalDir)
		if err != nil {
			packagesForOriginalPrefix = nil
		}
	}

	// Track whether the partial prefix resolves to a real package.
	originalPrefixExists := false
	if isPrefixPartial && originalPrefix != "" {
		for _, packageEntry := range packagesForOriginalPrefix {
			if packageEntry.Path == originalPrefix {
				originalPrefixExists = true
				break
			}
		}
	}

	selector := selection.New(nil, config.Global.Tags, config.Global.ExcludeTags, targetType)
	// Targets come from the exact package that the user is (implicitly) referring to.
	// For partial prefixes we only surface targets once the prefix resolves to a real package.
	var targets []string
	if isPrefixPartial && originalPrefixExists {
		targets = append(targets, collectTargets(packagesForOriginalPrefix, originalPrefix, targetPrefix, selector, isRelativeTarget)...)
	} else if !isPrefixPartial {
		targets = append(targets, collectTargets(packages, searchDirectory, targetPrefix, selector, isRelativeTarget)...)
	}

	// Directory suggestions are computed from two sources:
	// 1) Siblings under the search directory (root or parent).
	// 2) Child directories under the original prefix if it resolves to a package.
	directorySuggestions := make(map[string]bool)
	if !isRelativeTarget {
		skipExactPrefixDirectory := isPrefixPartial && originalPrefixExists && directoryPrefix != ""
		directorySuggestions = collectSiblingDirectories(packages, searchDirectory, directoryPrefix, skipExactPrefixDirectory)
		if isPrefixPartial && originalPrefixExists && originalPrefix != "" {
			mergeDirectorySuggestions(directorySuggestions, collectChildDirectories(packagesForOriginalPrefix, originalPrefix))
		}
	}

	var completions []string
	for fullPath, hasChildren := range directorySuggestions {
		completion := "//" + fullPath
		if isPrefixPartial && hasChildren {
			completion += "/"
		}
		completions = append(completions, completion)
	}

	// If only targets remain (no directories), add a trailing ":" to make it clear that
	// the next completion step is a target name rather than a package segment.
	shouldAddColonPrefix := isAbsolute &&
		!isRelativeTarget &&
		!strings.Contains(toComplete, ":") &&
		len(directorySuggestions) == 0 &&
		len(targets) > 0
	if shouldAddColonPrefix {
		targetPackagePrefix := searchDirectory
		if isPrefixPartial {
			targetPackagePrefix = originalPrefix
		}
		colonPrefix := "//" + targetPackagePrefix + ":"
		if targetPackagePrefix == "" {
			colonPrefix = "//:"
		}
		completions = append(completions, colonPrefix)
	}

	// If there is only a single target and no directory completions just offer that
	if len(completions) == 0 && len(targets) == 1 {
		return []string{targets[0]}, cobra.ShellCompDirectiveNoFileComp
	}

	completions = append(completions, targets...)
	sort.Strings(completions)
	debugToFile(fmt.Sprintf("completions: %s", completions))

	return completions, cobra.ShellCompDirectiveNoFileComp | cobra.ShellCompDirectiveNoSpace
}

func TestTargetPatternCompletion(command *cobra.Command, arguments []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	return TargetPatternCompletion(command, arguments, toComplete, selection.TestOnly)
}

func BuildTargetPatternCompletion(command *cobra.Command, arguments []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	return TargetPatternCompletion(command, arguments, toComplete, selection.NonTestOnly)
}

func BinaryTargetPatternCompletion(command *cobra.Command, arguments []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	return TargetPatternCompletion(command, arguments, toComplete, selection.BinOutput)
}

func AllTargetPatternCompletion(command *cobra.Command, arguments []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	return TargetPatternCompletion(command, arguments, toComplete, selection.AllTargets)
}

// collectTargets returns target and alias completions for the exact package path.
// The returned values already include package qualification unless relative targets were requested.
func collectTargets(packages []*model.Package, packagePath string, targetPrefix string, selector *selection.Selector, isRelativeTarget bool) []string {
	var targets []string
	for _, packageEntry := range packages {
		normalizedPath := normalizePackagePath(packageEntry.Path)
		if normalizedPath != packagePath {
			continue
		}
		for targetLabel, target := range packageEntry.Targets {
			if targetPrefix != "" && !strings.HasPrefix(targetLabel.Name, targetPrefix) {
				continue
			}
			if selector.Match(target) {
				targets = append(targets, formatTargetCompletion(targetLabel.String(), targetLabel.Name, isRelativeTarget))
			}
		}
		for aliasLabel := range packageEntry.Aliases {
			if targetPrefix != "" && !strings.HasPrefix(aliasLabel.Name, targetPrefix) {
				continue
			}
			targets = append(targets, formatTargetCompletion(aliasLabel.String(), aliasLabel.Name, isRelativeTarget))
		}
	}
	return targets
}

// collectSiblingDirectories returns directories that are siblings of the search directory.
// For the workspace root, siblings are the first path segment. Otherwise they are the
// first segment under searchDirectory.
func collectSiblingDirectories(packages []*model.Package, searchDirectory string, directoryPrefix string, skipExactPrefixDirectory bool) map[string]bool {
	directorySuggestions := make(map[string]bool)
	for _, packageEntry := range packages {
		packagePath := normalizePackagePath(packageEntry.Path)
		if packagePath == searchDirectory {
			continue
		}
		if searchDirectory == "" {
			segment := strings.Split(packagePath, "/")[0]
			if segment == "" {
				continue
			}
			if directoryPrefix == "" || strings.HasPrefix(segment, directoryPrefix) {
				if skipExactPrefixDirectory && segment == directoryPrefix {
					continue
				}
				addDirectorySuggestion(directorySuggestions, segment, strings.Contains(packagePath, "/"))
			}
			continue
		}
		if strings.HasPrefix(packagePath, searchDirectory+"/") {
			rest := strings.TrimPrefix(packagePath, searchDirectory+"/")
			segment := strings.Split(rest, "/")[0]
			if directoryPrefix == "" || strings.HasPrefix(segment, directoryPrefix) {
				if skipExactPrefixDirectory && segment == directoryPrefix {
					continue
				}
				addDirectorySuggestion(directorySuggestions, searchDirectory+"/"+segment, strings.Contains(rest, "/"))
			}
		}
	}
	return directorySuggestions
}

// collectChildDirectories returns immediate child directories under the given prefix.
func collectChildDirectories(packages []*model.Package, prefix string) map[string]bool {
	directorySuggestions := make(map[string]bool)
	for _, packageEntry := range packages {
		packagePath := normalizePackagePath(packageEntry.Path)
		if !strings.HasPrefix(packagePath, prefix+"/") {
			continue
		}
		rest := strings.TrimPrefix(packagePath, prefix+"/")
		segment := strings.Split(rest, "/")[0]
		fullPath := prefix + "/" + segment
		addDirectorySuggestion(directorySuggestions, fullPath, strings.Contains(rest, "/"))
	}
	return directorySuggestions
}

func mergeDirectorySuggestions(target map[string]bool, additions map[string]bool) {
	for path, hasChildren := range additions {
		if target[path] {
			continue
		}
		target[path] = hasChildren
	}
}

func addDirectorySuggestion(target map[string]bool, fullPath string, hasChildren bool) {
	if existing, ok := target[fullPath]; ok && existing {
		return
	}
	target[fullPath] = hasChildren
}

func normalizePackagePath(path string) string {
	if path == "." {
		return ""
	}
	return path
}

func formatTargetCompletion(absoluteLabel string, targetName string, isRelativeTarget bool) string {
	if isRelativeTarget {
		return ":" + targetName
	}
	return absoluteLabel
}

func splitPartialPrefix(prefix string) (string, string) {
	if prefix == "" {
		return "", ""
	}
	if strings.Contains(prefix, "/") {
		baseDirectory := prefix[:strings.LastIndex(prefix, "/")]
		if baseDirectory == "." {
			baseDirectory = ""
		}
		return baseDirectory, prefix[strings.LastIndex(prefix, "/")+1:]
	}
	return "", prefix
}

func debugToFile(msg string) {
	if config.Global.DebugCompletion {
		_ = os.WriteFile("/tmp/grog-completion.log", []byte(msg+"\n"), 0o644)
	}
}
