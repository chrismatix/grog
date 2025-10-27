package completions

import (
	"grog/internal/config"
	"grog/internal/console"
	"grog/internal/label"
	"grog/internal/loading"
	"grog/internal/selection"
	"sort"
	"strings"

	"github.com/spf13/cobra"
)

func TargetPatternCompletion(_ *cobra.Command, _ []string, toComplete string, targetType selection.TargetTypeSelection) ([]string, cobra.ShellCompDirective) {
	ctx, _ := console.SetupCommand()
	currentPkg, err := config.Global.GetCurrentPackage()
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp | cobra.ShellCompDirectiveError
	}

	pattern := label.ParsePartialTargetPattern(currentPkg, toComplete)
	searchDir := pattern.Prefix()
	if searchDir == "" && !strings.HasPrefix(toComplete, "//") {
		searchDir = currentPkg
	}

	absoluteSearchDir := config.GetPathAbsoluteToWorkspaceRoot(searchDir)
	packages, err := loading.LoadPackages(ctx, absoluteSearchDir)
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp | cobra.ShellCompDirectiveError
	}

	selector := selection.New(nil, config.Global.Tags, config.Global.ExcludeTags, targetType)
	dirs := make(map[string]struct{})
	var targets []string

	for _, pkg := range packages {
		pkgPath := pkg.Path
		if pkgPath == "." {
			pkgPath = ""
		}

		if pkgPath == searchDir {
			for targetLabel, target := range pkg.Targets {
				if pattern.Target() != "" && !strings.HasPrefix(targetLabel.Name, pattern.Target()) {
					continue
				}
				if selector.Match(target) {
					targets = append(targets, targetLabel.String())
				}
			}
			for aliasLabel := range pkg.Aliases {
				if pattern.Target() != "" && !strings.HasPrefix(aliasLabel.Name, pattern.Target()) {
					continue
				}
				targets = append(targets, aliasLabel.String())
			}
			continue
		}
		if searchDir == "" {
			// We are at the root package so just add the directory
			segment := strings.Split(pkgPath, "/")[0]
			dirs[segment] = struct{}{}
		} else if strings.HasPrefix(pkgPath, searchDir) {
			// pkgPath searchDir/foo/bar/bar
			rest := strings.TrimPrefix(pkgPath, searchDir+"/")
			// rest is foo/bar/bar
			segment := strings.Split(rest, "/")[0]
			// segment is foo
			if pattern.Target() == "" || strings.HasPrefix(segment, pattern.Target()) {
				dirs[segment] = struct{}{}
			}
		}
	}

	var completions []string
	for directory := range dirs {
		path := directory
		if searchDir != "" {
			path = searchDir + "/" + directory
		}
		completions = append(completions, "//"+path)
	}

	// If there is only a single target and no directory completions just offer that
	if len(completions) == 0 && len(targets) == 1 {
		return []string{targets[0]}, cobra.ShellCompDirectiveNoFileComp
	}

	completions = append(completions, targets...)
	sort.Strings(completions)

	return completions, cobra.ShellCompDirectiveNoFileComp | cobra.ShellCompDirectiveNoSpace
}

func TestTargetPatternCompletion(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	return TargetPatternCompletion(cmd, args, toComplete, selection.TestOnly)
}

func BuildTargetPatternCompletion(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	return TargetPatternCompletion(cmd, args, toComplete, selection.NonTestOnly)
}

func BinaryTargetPatternCompletion(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	return TargetPatternCompletion(cmd, args, toComplete, selection.BinOutput)
}

func AllTargetPatternCompletion(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	return TargetPatternCompletion(cmd, args, toComplete, selection.AllTargets)
}
