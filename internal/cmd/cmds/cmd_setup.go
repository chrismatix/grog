package cmds

import (
	"context"
	"go.uber.org/zap"
	"grog/internal/console"
	"os"
	"os/signal"
	"syscall"
)

// setupCommand universal helper for setting up the context and logger for each command
func setupCommand() (context.Context, *zap.SugaredLogger) {
	ctx, cancel := context.WithCancel(context.Background())
	// Listen for SIGTERM or SIGINT to cancel the context
	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, os.Interrupt, syscall.SIGTERM)
	go func() {
		select {
		case <-signalChan:
			cancel()
		case <-ctx.Done():
		}
	}()

	logger := console.GetLogger(ctx)
	return console.WithLogger(ctx, logger), logger
}
