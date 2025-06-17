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
       searchDir := pattern.Prefix()
       if searchDir == "" && !strings.HasPrefix(toComplete, "//") {
               searchDir = currentPkg
       }

	packages, err := loading.LoadPackages(ctx, searchDir)
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	dirs := make(map[string]struct{})
	var targets []string

	for _, pkg := range packages {
		pkgPath, err := config.GetPackagePath(pkg.SourceFilePath)
		if err != nil {
			continue
		}
		if pkgPath == "." {
			pkgPath = ""
		}

		if pkgPath == searchDir {
                       for lbl := range pkg.Targets {
                               if pattern.Target() != "" && !strings.HasPrefix(lbl.Name, pattern.Target()) {
                                       continue
                               }
                               targets = append(targets, lbl.String())
                       }
                       continue
               }
               if searchDir == "" {
                       seg := strings.Split(pkgPath, "/")[0]
                       if seg != pkgPath {
                               dirs[seg] = struct{}{}
                       }
               } else if strings.HasPrefix(pkgPath, searchDir+"/") {
                       rest := strings.TrimPrefix(pkgPath, searchDir+"/")
                       seg := strings.Split(rest, "/")[0]
                       dirs[seg] = struct{}{}
               }
       }

       // If the user already typed a full target name, suggest only that target
       if pattern.Target() != "" {
               for _, t := range targets {
                       if strings.HasSuffix(t, ":"+pattern.Target()) {
                               return []string{t}, cobra.ShellCompDirectiveNoFileComp
                       }
               }
       }

       var comps []string
       for d := range dirs {
               p := d
               if searchDir != "" {
                       p = searchDir + "/" + d
               }
               comps = append(comps, "//"+p+"/")
       }
       comps = append(comps, targets...)
	sort.Strings(comps)

	return comps, cobra.ShellCompDirectiveNoFileComp
}

func TargetLabelCompletion(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	return TargetPatternCompletion(cmd, args, toComplete)
}
