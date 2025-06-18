package console

import (
	"context"
	"go.uber.org/zap"
	"os"
	"os/signal"
	"syscall"
)

// SetupCommand universal helper for setting up the context and logger for each command
func SetupCommand() (context.Context, *zap.SugaredLogger) {
	ctx, cancel := context.WithCancel(context.Background())
	// Listen for SIGTERM or SIGINT to cancel the context
	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, os.Interrupt, syscall.SIGTERM)
	go func() {
		select {
		case sig := <-signalChan:
			GetLogger(ctx).Infof("Received signal %v, exiting...", sig)
			cancel()
		case <-ctx.Done():
		}
	}()

	logger := GetLogger(ctx)
	return WithLogger(ctx, logger), logger
}
