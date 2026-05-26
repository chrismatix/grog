package backends

import (
	"context"
	"io"
	"runtime"
	"sync"
	"sync/atomic"

	"golang.org/x/sync/semaphore"
)

// Default bounds for the global I/O concurrency limit.
//
// Lower bound (32): keeps an S3-style HTTP connection saturated under
// typical RTT (Little's law: ~50ms latency × ~600 req/s ≈ 30 in flight).
// Upper bound (256): stays under Go's 10k-thread ceiling and default FD
// limits even when CAS, target/taint cache, and the docker proxy share
// the budget.
//
// Between the bounds the default scales with NumCPU * 4, preserving PR
// #100's heuristic for mid-sized machines.
const (
	defaultIOConcurrencyMin = 32
	defaultIOConcurrencyMax = 256
)

// DefaultIOConcurrency returns NumCPU * 4 clamped to
// [defaultIOConcurrencyMin, defaultIOConcurrencyMax].
func DefaultIOConcurrency() int {
	n := runtime.NumCPU() * 4
	if n < defaultIOConcurrencyMin {
		return defaultIOConcurrencyMin
	}
	if n > defaultIOConcurrencyMax {
		return defaultIOConcurrencyMax
	}
	return n
}

// globalIOSem bounds concurrent backend I/O process-wide. Initialised via
// SetGlobalIOConcurrency before the backend is used; when nil (e.g. tests
// that bypass GetCacheBackend) BoundedBackend degrades to a passthrough.
// globalIOCap mirrors the configured capacity so callers that fan out their
// own goroutines (e.g. directory uploads) can size their concurrency to the
// same budget via GlobalIOConcurrency.
var (
	globalIOSemMu sync.Mutex
	globalIOSem   atomic.Pointer[semaphore.Weighted]
	globalIOCap   atomic.Int64
)

// SetGlobalIOConcurrency installs (or replaces) the process-wide I/O
// semaphore. A non-positive capacity disables the limit.
func SetGlobalIOConcurrency(capacity int) {
	globalIOSemMu.Lock()
	defer globalIOSemMu.Unlock()
	if capacity <= 0 {
		globalIOSem.Store(nil)
		globalIOCap.Store(0)
		return
	}
	globalIOSem.Store(semaphore.NewWeighted(int64(capacity)))
	globalIOCap.Store(int64(capacity))
}

// GlobalIOConcurrency reports the configured process-wide I/O cap, falling
// back to DefaultIOConcurrency when the limit is unset. Use it to bound a
// goroutine fan-out that performs backend I/O so the fan-out can't open more
// resources (e.g. file descriptors) than the backend will service at once.
func GlobalIOConcurrency() int {
	if c := globalIOCap.Load(); c > 0 {
		return int(c)
	}
	return DefaultIOConcurrency()
}

// acquireForBackend is the internal Acquire used by BoundedBackend ops.
func acquireForBackend(ctx context.Context) (func(), error) {
	sem := globalIOSem.Load()
	if sem == nil {
		return func() {}, nil
	}
	if err := sem.Acquire(ctx, 1); err != nil {
		return nil, err
	}
	var released atomic.Bool
	return func() {
		if released.CompareAndSwap(false, true) {
			sem.Release(1)
		}
	}, nil
}

// BoundedBackend wraps a CacheBackend so every op acquires a slot on the
// process-wide I/O semaphore. Get holds the slot for the reader's lifetime,
// so the cap reflects in-flight bytes — not just call sites.
type BoundedBackend struct {
	inner CacheBackend
}

func NewBoundedBackend(inner CacheBackend) *BoundedBackend {
	return &BoundedBackend{inner: inner}
}

// Inner returns the wrapped backend (e.g. for tests).
func (b *BoundedBackend) Inner() CacheBackend {
	return b.inner
}

func (b *BoundedBackend) TypeName() string {
	return b.inner.TypeName()
}

func (b *BoundedBackend) Get(ctx context.Context, path, key string) (io.ReadCloser, error) {
	release, err := acquireForBackend(ctx)
	if err != nil {
		return nil, err
	}
	rc, err := b.inner.Get(ctx, path, key)
	if err != nil {
		release()
		return nil, err
	}
	return &releasingReadCloser{ReadCloser: rc, release: release}, nil
}

func (b *BoundedBackend) Set(ctx context.Context, path, key string, content io.Reader) error {
	release, err := acquireForBackend(ctx)
	if err != nil {
		return err
	}
	defer release()
	return b.inner.Set(ctx, path, key, content)
}

// BeginWrite is intentionally NOT bounded. The dockerproxy registry holds
// staged writes across POST/PATCH/.../PUT, so a global slot held for the
// whole session would leak on interrupted uploads until concurrent docker
// pushes blocked the rest of the cache. Bytes through the staged writer
// reach the backend via a background pipe goroutine; the natural rate
// limit there is the caller and the pipe buffer, not this semaphore.
func (b *BoundedBackend) BeginWrite(ctx context.Context) (StagedWriter, error) {
	return b.inner.BeginWrite(ctx)
}

func (b *BoundedBackend) Delete(ctx context.Context, path, key string) error {
	release, err := acquireForBackend(ctx)
	if err != nil {
		return err
	}
	defer release()
	return b.inner.Delete(ctx, path, key)
}

func (b *BoundedBackend) Exists(ctx context.Context, path, key string) (bool, error) {
	release, err := acquireForBackend(ctx)
	if err != nil {
		return false, err
	}
	defer release()
	return b.inner.Exists(ctx, path, key)
}

func (b *BoundedBackend) Size(ctx context.Context, path, key string) (int64, error) {
	release, err := acquireForBackend(ctx)
	if err != nil {
		return 0, err
	}
	defer release()
	return b.inner.Size(ctx, path, key)
}

func (b *BoundedBackend) ListKeys(ctx context.Context, path, suffix string) ([]string, error) {
	release, err := acquireForBackend(ctx)
	if err != nil {
		return nil, err
	}
	defer release()
	return b.inner.ListKeys(ctx, path, suffix)
}

// releasingReadCloser releases its semaphore slot on Close. Idempotent.
type releasingReadCloser struct {
	io.ReadCloser

	release func()
}

func (r *releasingReadCloser) Close() error {
	err := r.ReadCloser.Close()
	r.release()
	return err
}
