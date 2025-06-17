package completions

import (
	"context"
	"github.com/spf13/cobra"
	"grog/internal/config"
	"grog/internal/label"
	"grog/internal/loading"
	"sort"
	"strings"
)

func TargetPatternCompletion(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	ctx := context.Background()
	currentPkg, err := config.Global.GetCurrentPackage()
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	pattern := label.ParsePartialTargetPattern(currentPkg, toComplete)
	dir := pattern.Prefix
	if dir == "" && !strings.HasPrefix(toComplete, "//") {
		dir = currentPkg
	}

	packages, err := loading.LoadPackages(ctx, dir)
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	var comps []string
	for _, pkg := range packages {
		for lbl := range pkg.Targets {
			if pattern.Prefix != "" && !strings.HasPrefix(lbl.Package, pattern.Prefix) {
				continue
			}
			if pattern.TargetPattern != "" && !strings.HasPrefix(lbl.Name, pattern.TargetPattern) {
				continue
			}
			comps = append(comps, lbl.String())
		}
	}
	sort.Strings(comps)
	return comps, cobra.ShellCompDirectiveNoFileComp
}

func TargetLabelCompletion(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	return TargetPatternCompletion(cmd, args, toComplete)
}
