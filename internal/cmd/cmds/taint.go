package cmds

import (
	"github.com/spf13/cobra"
	"grog/internal/caching"
	"grog/internal/caching/backends"
	"grog/internal/completions"
	"grog/internal/config"
	"grog/internal/console"
	"grog/internal/hashing"
	"grog/internal/label"
	"grog/internal/loading"
	"grog/internal/model"
	"grog/internal/selection"
)

var TaintCmd = &cobra.Command{
	Use:   "taint",
	Short: "Taints targets by pattern to force execution regardless of cache status.",
	Long: `Marks specified targets as "tainted", which forces them to be rebuilt on the next build command,
regardless of whether they would normally be considered up-to-date according to the cache.
This is useful when you want to force a rebuild of specific targets.`,
	Example: `  grog taint //path/to/package:target      # Taint a specific target
  grog taint //path/to/package/...         # Taint all targets in a package and subpackages
  grog taint //path/to/package:*           # Taint all targets in a package`,
	Args:              cobra.MinimumNArgs(1),
	ValidArgsFunction: completions.AllTargetPatternCompletion,
	Run: func(cmd *cobra.Command, args []string) {
		ctx, logger := console.SetupCommand()

		currentPackagePath, err := config.Global.GetCurrentPackage()
		if err != nil {
			logger.Fatalf("could not get current package: %v", err)
		}

		targetPatterns, err := label.ParsePatternsOrMatchAll(currentPackagePath, args)
		if err != nil {
			logger.Fatalf("could not parse target pattern: %v", err)
		}

		graph := loading.MustLoadGraphForQuery(ctx, logger)

		selector := selection.New(targetPatterns, config.Global.Tags, config.Global.ExcludeTags, selection.AllTargets)
		selector.SelectTargets(graph)

		selectedNodes := graph.GetSelectedNodes()

		// Initialize cache
		cache, err := backends.GetCacheBackend(ctx, config.Global.Cache)
		if err != nil {
			logger.Fatalf("could not instantiate cache: %v", err)
		}
		targetCache := caching.NewTargetCache(cache)

		taintedCount := 0
		targetHasher := hashing.NewTargetHasher(graph)
		for _, node := range selectedNodes {
			target, ok := node.(*model.Target)
			if !ok {
				continue
			}

			// In order for the cache key to be correct the ChangeHash needs to be set
			err := targetHasher.SetTargetChangeHash(target)
			err = targetCache.Taint(ctx, *target)
			if err != nil {
				logger.Errorf("Failed to taint target %s: %v", target.Label, err)
				continue
			}
			taintedCount++
			logger.Debugf("Tainted target: %s", target.Label)
		}

		logger.Infof("Tainted %s", console.FCountTargets(taintedCount))
	},
}
