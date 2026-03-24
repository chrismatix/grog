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

// CacheWriter bridges the gap between output preparation (which must be synchronous) and
// cache persistence (which can be deferred).
//
// When a target finishes executing, its output handler's Write() method runs on the task
// worker to compute output hashes and stage immutable snapshots of the output data. This
// produces a PreparedTargetResult containing the output hash (needed immediately by the
// DAG walker to unblock downstream targets) and a set of OutputWritePlans that capture
// the actual I/O work — uploading staged files to the CAS, pushing Docker images, etc.
//
// CacheWriter decides where that I/O work runs:
//
//   - Sync mode (asyncWrites=false): write plans execute inline on the task worker.
//     The worker blocks until all uploads and the target cache entry are written.
//     Failures are fatal and propagated to the build.
//
//   - Async mode (asyncWrites=true): write plans are submitted to a dedicated I/O worker
//     pool via RunFireAndForget. The task worker returns immediately after hash computation,
//     freeing it for the next target. I/O workers run with a non-cancellable context so
//     cache writes can finish even if the build is interrupted. Failures are logged as
//     warnings — a failed cache write only means the next build won't get a cache hit.
//
// After all write plans in a batch succeed, CacheWriter writes the TargetResult proto
// to the target cache (the metadata index that maps input hashes to output hashes).
// Cleanup() is called on every write plan regardless of outcome to remove staged temp files.
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
		if err := writePlan.Execute(ctx, progress); err != nil {
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
