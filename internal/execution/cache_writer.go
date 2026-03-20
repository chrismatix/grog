package execution

import (
	"context"
	"fmt"

	"grog/internal/caching"
	"grog/internal/console"
	"grog/internal/output"
	"grog/internal/output/handlers"
	"grog/internal/worker"
)

// CacheWriter owns persistence policy for prepared target results.
type CacheWriter struct {
	targetCache *caching.TargetResultCache
	ioPool      *worker.TaskWorkerPool[struct{}]
	asyncWrites bool
	ioContext   context.Context
}

func NewCacheWriter(
	targetCache *caching.TargetResultCache,
	ioPool *worker.TaskWorkerPool[struct{}],
	asyncWrites bool,
	ioContext context.Context,
) *CacheWriter {
	return &CacheWriter{
		targetCache: targetCache,
		ioPool:      ioPool,
		asyncWrites: asyncWrites,
		ioContext:   ioContext,
	}
}

func (c *CacheWriter) PersistPreparedTarget(
	ctx context.Context,
	targetLabel string,
	preparedTarget *output.PreparedTargetResult,
	update worker.StatusFunc,
) error {
	if preparedTarget == nil {
		return fmt.Errorf("prepared target result for %s is nil", targetLabel)
	}

	if !c.asyncWrites || len(preparedTarget.WritePlans) == 0 {
		progress := worker.NewProgressTracker(
			fmt.Sprintf("%s: writing cache", targetLabel),
			0,
			update,
		)
		return c.commit(ctx, targetLabel, preparedTarget, progress, true)
	}

	err := c.ioPool.RunFireAndForget(func(ioUpdate worker.StatusFunc) (struct{}, error) {
		progress := worker.NewProgressTracker(
			fmt.Sprintf("%s: writing cache", targetLabel),
			0,
			ioUpdate,
		)
		if commitErr := c.commit(c.ioContext, targetLabel, preparedTarget, progress, false); commitErr != nil {
			console.GetLogger(c.ioContext).Warnf("async cache write error for %s (non-fatal): %v", targetLabel, commitErr)
		}
		return struct{}{}, nil
	})
	if err != nil {
		c.cleanupPlans(ctx, preparedTarget.WritePlans)
		return err
	}
	return nil
}

func (c *CacheWriter) Wait() {
	c.ioPool.WaitForCompletion()
}

func (c *CacheWriter) commit(
	ctx context.Context,
	targetLabel string,
	preparedTarget *output.PreparedTargetResult,
	progress *worker.ProgressTracker,
	failureIsFatal bool,
) error {
	for _, writePlan := range preparedTarget.WritePlans {
		if err := writePlan.Upload(ctx, progress); err != nil {
			c.cleanupPlans(ctx, preparedTarget.WritePlans)
			if failureIsFatal {
				return err
			}
			return fmt.Errorf("skipping target cache publication for %s: %w", targetLabel, err)
		}
	}

	if err := c.targetCache.Write(ctx, preparedTarget.TargetResult); err != nil {
		c.cleanupPlans(ctx, preparedTarget.WritePlans)
		return err
	}

	c.cleanupPlans(ctx, preparedTarget.WritePlans)
	return nil
}

func (c *CacheWriter) cleanupPlans(ctx context.Context, writePlans []handlers.OutputWritePlan) {
	for _, writePlan := range writePlans {
		if err := writePlan.Cleanup(ctx); err != nil {
			console.GetLogger(ctx).Warnf("failed to clean up cache write plan: %v", err)
		}
	}
}
