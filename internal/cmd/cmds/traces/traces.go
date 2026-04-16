package traces

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"grog/internal/caching/backends"
	"grog/internal/config"
	"grog/internal/console"
	"grog/internal/tracing"
)

var Cmd = &cobra.Command{
	Use:   "traces",
	Short: "View and manage build execution traces.",
	Long:  `View, analyze, and export build execution traces for performance analysis and dashboard integration.`,
}

func AddCmd(rootCmd *cobra.Command) {
	registerListCmd()
	registerShowCmd()
	registerStatsCmd()
	registerPullCmd()
	registerExportCmd()
	registerPruneCmd()
	rootCmd.AddCommand(Cmd)
}

func getStore(ctx context.Context, logger *console.Logger) *tracing.TraceStore {
	cacheConfig := config.Global.Cache
	if config.Global.Traces.Backend != "" {
		cacheConfig = config.CacheConfig{
			Backend: config.Global.Traces.Backend,
			GCS:     config.Global.Traces.GCS,
			S3:      config.Global.Traces.S3,
		}
	}

	cache, err := backends.GetCacheBackend(ctx, cacheConfig)
	if err != nil {
		logger.Fatalf("could not instantiate cache backend for traces: %v", err)
	}

	resolver := tracing.NewPathResolver()
	store, err := tracing.NewTraceStore(cache, resolver)
	if err != nil {
		logger.Fatalf("could not initialize trace store: %v", err)
	}
	return store
}

func normalizeCommand(command string) (string, error) {
	switch command {
	case "":
		return "", nil
	case "build", "test", "run":
		return command, nil
	default:
		return "", fmt.Errorf("invalid command %q (use build, test, or run)", command)
	}
}

func normalizeStatsCommandType(commandType string) (string, error) {
	switch commandType {
	case "", "all":
		return "", nil
	case "build", "test":
		return commandType, nil
	default:
		return "", fmt.Errorf("invalid command type %q (use build, test, or all)", commandType)
	}
}

func normalizeStatsCI(ciValue string) (*bool, error) {
	switch ciValue {
	case "", "all":
		return nil, nil
	case "true":
		isCI := true
		return &isCI, nil
	case "false":
		isCI := false
		return &isCI, nil
	default:
		return nil, fmt.Errorf("invalid ci filter %q (use true, false, or all)", ciValue)
	}
}
