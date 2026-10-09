package completions

import (
	"context"
	"fmt"
	os "os"
	"slices"
	"strings"

	"github.com/chrismatix/grog/internal/config"
	"github.com/chrismatix/grog/internal/console"
	"github.com/chrismatix/grog/internal/label"
	"github.com/chrismatix/grog/internal/loading"
	"github.com/chrismatix/grog/internal/model"
	"github.com/chrismatix/grog/internal/selection"

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
	targetPrefix := pattern.Target()
	isPrefixPartial := pattern.IsPrefixPartial()
	searchDirectory, directoryPrefix := resolveSearchDirectory(pattern, currentPackage, isAbsolute)

	debugToFile(fmt.Sprintf("searchDir: %s\n", searchDirectory))
	debugToFile(fmt.Sprintf("target: %s\n", pattern.Target()))

	// Load packages from the search directory, which is either the current package,
	// the workspace root, or the parent directory for partial prefixes.
	packages, err := loading.LoadPackages(context, config.GetPathAbsoluteToWorkspaceRoot(searchDirectory))
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp | cobra.ShellCompDirectiveError
	}

	// The package the user is (implicitly) referring to: the search directory, or the typed
	// prefix itself when it is partial. An unresolved partial prefix has no packages in scope,
	// so it only gets directory suggestions.
	packagePath, packagesInScope, originalPrefixExists := searchDirectory, packages, false
	if isPrefixPartial {
		packagePath = originalPrefix
		packagesInScope, originalPrefixExists = loadOriginalPrefixPackages(context, originalPrefix, searchDirectory, packages)
	}

	selector := selection.New(nil, config.Global.Tags, config.Global.ExcludeTags, targetType)
	targets := collectTargets(packagesInScope, packagePath, targetPrefix, selector, isRelativeTarget)

	// Directory suggestions are computed from two sources:
	// 1) Siblings under the search directory (root or parent).
	// 2) Child directories under the original prefix if it resolves to a package.
	var completions []string
	var directorySuggestions map[string]bool
	if !isRelativeTarget {
		directorySuggestions = collectSiblingDirectories(packages, searchDirectory, directoryPrefix, originalPrefixExists && directoryPrefix != "")
		if originalPrefixExists {
			mergeDirectorySuggestions(directorySuggestions, collectChildDirectories(packagesInScope, packagePath))
		}
		completions = directoryCompletions(directorySuggestions, isPrefixPartial)
		if packageHasChildDirectories(packagesInScope, packagePath) {
			if packagePath == "" {
				completions = append(completions, "//...")
			} else {
				completions = append(completions, "//"+packagePath+"/...")
			}
		}
	}

	// If only targets remain (no directories), add a trailing ":" to make it clear that
	// the next completion step is a target name rather than a package segment.
	if isAbsolute && !strings.Contains(toComplete, ":") && len(directorySuggestions) == 0 && len(targets) > 0 {
		completions = append(completions, "//"+packagePath+":")
	}

	if packageHasMatchingTargets(packagesInScope, packagePath, selector) {
		if isRelativeTarget {
			completions = append(completions, ":...")
		} else {
			completions = append(completions, "//"+packagePath+":...")
		}
	}

	// If there is only a single target and no directory completions just offer that
	if len(completions) == 0 && len(targets) == 1 {
		return []string{targets[0]}, cobra.ShellCompDirectiveNoFileComp
	}

	completions = append(completions, targets...)
	slices.Sort(completions)
	completions = slices.Compact(completions)
	debugToFile(fmt.Sprintf("completions: %s", completions))

	return completions, cobra.ShellCompDirectiveNoFileComp | cobra.ShellCompDirectiveNoSpace
}

// resolveSearchDirectory returns the directory to load packages from and the directory
// name fragment that suggestions must start with.
func resolveSearchDirectory(pattern label.TargetPattern, currentPackage string, isAbsolute bool) (string, string) {
	searchDirectory := pattern.Prefix()
	directoryPrefix := pattern.Target()
	// When the prefix is partial, re-scope the search to the parent directory so we can
	// suggest siblings that share the same leading segment(s). Example:
	// - Input: "//pack"
	// - Prefix(): "pack"
	// - Search should happen at root, with "pack" used as the directory prefix filter.
	if pattern.IsPrefixPartial() && isAbsolute {
		searchDirectory, directoryPrefix = splitPartialPrefix(searchDirectory)
	}
	// Relative inputs without a package prefix should resolve from the current package.
	if searchDirectory == "" && !isAbsolute {
		searchDirectory = currentPackage
	}
	return searchDirectory, directoryPrefix
}

// loadOriginalPrefixPackages loads the packages beneath a partially typed prefix, or
// returns nil when the prefix is not itself a package.
func loadOriginalPrefixPackages(commandContext context.Context, originalPrefix string, searchDirectory string, packages []*model.Package) ([]*model.Package, bool) {
	if originalPrefix == "" {
		return nil, false
	}
	if originalPrefix != searchDirectory {
		var err error
		packages, err = loading.LoadPackages(commandContext, config.GetPathAbsoluteToWorkspaceRoot(originalPrefix))
		if err != nil {
			return nil, false
		}
	}
	for _, packageEntry := range packages {
		if packageEntry.Path == originalPrefix {
			return packages, true
		}
	}
	return nil, false
}

// directoryCompletions formats directory suggestions as "//dir" (with a trailing "/" for
// partial prefixes that have children) plus a "//dir/..." wildcard for each directory with children.
func directoryCompletions(directorySuggestions map[string]bool, isPrefixPartial bool) []string {
	var completions []string
	for fullPath, hasChildren := range directorySuggestions {
		completion := "//" + fullPath
		if isPrefixPartial && hasChildren {
			completion += "/"
		}
		completions = append(completions, completion)
		if hasChildren {
			completions = append(completions, "//"+fullPath+"/...")
		}
	}
	return completions
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

func packageHasMatchingTargets(packages []*model.Package, packagePath string, selector *selection.Selector) bool {
	for _, packageEntry := range packages {
		normalizedPath := normalizePackagePath(packageEntry.Path)
		if normalizedPath != packagePath {
			continue
		}
		for _, target := range packageEntry.Targets {
			if selector.Match(target) {
				return true
			}
		}
	}
	return false
}

func packageHasChildDirectories(packages []*model.Package, packagePath string) bool {
	normalizedPrefix := normalizePackagePath(packagePath)
	prefixWithSeparator := normalizedPrefix
	if prefixWithSeparator != "" {
		prefixWithSeparator += "/"
	}
	for _, packageEntry := range packages {
		normalizedPath := normalizePackagePath(packageEntry.Path)
		if normalizedPrefix == "" {
			if normalizedPath != "" {
				return true
			}
			continue
		}
		if strings.HasPrefix(normalizedPath, prefixWithSeparator) {
			return true
		}
	}
	return false
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
			segment, _, _ := strings.Cut(packagePath, "/")
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
		if after, ok := strings.CutPrefix(packagePath, searchDirectory+"/"); ok {
			rest := after
			segment, _, _ := strings.Cut(rest, "/")
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
		segment, _, _ := strings.Cut(rest, "/")
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
