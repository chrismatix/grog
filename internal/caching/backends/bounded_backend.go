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
// The lower bound (32) ensures we have enough in-flight requests to keep a
// single S3-style HTTP connection saturated under typical RTT (Little's law:
// for ~50ms latency and ~600 req/s throughput a connection wants ~30 in
// flight). Going below this on small machines starves remote backends.
//
// The upper bound (256) keeps us well under Go's 10k-thread ceiling and the
// default open-file-descriptor limit on macOS/Linux even when several CAS
// reads share the cap with target/taint cache traffic and the docker proxy.
//
// The default scales with NumCPU * 4 between those bounds, which preserves
// the heuristic from PR #100 for mid-sized machines while clamping the
// extremes where it produced either too little parallelism (1-2 cores) or
// dangerously much (96+ cores).
const (
	defaultIOConcurrencyMin = 32
	defaultIOConcurrencyMax = 256
)

// DefaultIOConcurrency returns the default global I/O concurrency cap derived
// from the host CPU count, clamped to [defaultIOConcurrencyMin,
// defaultIOConcurrencyMax].
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

// globalIOSem is the process-wide semaphore that bounds concurrent backend
// I/O operations. It is initialised once via SetGlobalIOConcurrency before
// the backend is used; tests that bypass GetCacheBackend leave it nil and
// the BoundedBackend wrapper degrades to a passthrough.
var (
	globalIOSemMu sync.Mutex
	globalIOSem   atomic.Pointer[semaphore.Weighted]
)

// SetGlobalIOConcurrency installs (or replaces) the process-wide I/O
// semaphore with the given capacity. A non-positive capacity disables the
// limit (useful for tests). Safe to call multiple times; later calls
// overwrite earlier ones.
func SetGlobalIOConcurrency(cap int) {
	globalIOSemMu.Lock()
	defer globalIOSemMu.Unlock()
	if cap <= 0 {
		globalIOSem.Store(nil)
		return
	}
	globalIOSem.Store(semaphore.NewWeighted(int64(cap)))
}

// acquireGlobalIO blocks until a slot is available on the global semaphore
// or ctx is cancelled. It returns a release func; the func is a no-op when
// the semaphore is unset.
func acquireGlobalIO(ctx context.Context) (func(), error) {
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

// BoundedBackend wraps a CacheBackend so every operation acquires a slot on
// the process-wide I/O semaphore. Streaming operations (Get, BeginWrite)
// hold the slot for the lifetime of the returned reader/writer, so the cap
// reflects in-flight bytes — not just call sites.
type BoundedBackend struct {
	inner CacheBackend
}

// NewBoundedBackend wraps the given backend with the global I/O semaphore.
func NewBoundedBackend(inner CacheBackend) *BoundedBackend {
	return &BoundedBackend{inner: inner}
}

// Inner returns the wrapped backend. Used by callers that need to reach
// past the bound (e.g. tests).
func (b *BoundedBackend) Inner() CacheBackend {
	return b.inner
}

func (b *BoundedBackend) TypeName() string {
	return b.inner.TypeName()
}

func (b *BoundedBackend) Get(ctx context.Context, path, key string) (io.ReadCloser, error) {
	release, err := acquireGlobalIO(ctx)
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
	release, err := acquireGlobalIO(ctx)
	if err != nil {
		return err
	}
	defer release()
	return b.inner.Set(ctx, path, key, content)
}

func (b *BoundedBackend) BeginWrite(ctx context.Context) (StagedWriter, error) {
	release, err := acquireGlobalIO(ctx)
	if err != nil {
		return nil, err
	}
	sw, err := b.inner.BeginWrite(ctx)
	if err != nil {
		release()
		return nil, err
	}
	return &releasingStagedWriter{StagedWriter: sw, release: release}, nil
}

func (b *BoundedBackend) Delete(ctx context.Context, path, key string) error {
	release, err := acquireGlobalIO(ctx)
	if err != nil {
		return err
	}
	defer release()
	return b.inner.Delete(ctx, path, key)
}

func (b *BoundedBackend) Exists(ctx context.Context, path, key string) (bool, error) {
	release, err := acquireGlobalIO(ctx)
	if err != nil {
		return false, err
	}
	defer release()
	return b.inner.Exists(ctx, path, key)
}

func (b *BoundedBackend) Size(ctx context.Context, path, key string) (int64, error) {
	release, err := acquireGlobalIO(ctx)
	if err != nil {
		return 0, err
	}
	defer release()
	return b.inner.Size(ctx, path, key)
}

func (b *BoundedBackend) ListKeys(ctx context.Context, path, suffix string) ([]string, error) {
	release, err := acquireGlobalIO(ctx)
	if err != nil {
		return nil, err
	}
	defer release()
	return b.inner.ListKeys(ctx, path, suffix)
}

// releasingReadCloser releases its semaphore slot when Close is called. The
// release is idempotent.
type releasingReadCloser struct {
	io.ReadCloser
	release func()
}

func (r *releasingReadCloser) Close() error {
	err := r.ReadCloser.Close()
	r.release()
	return err
}

// releasingStagedWriter holds the semaphore slot for the lifetime of the
// streaming write and releases it on Commit or Cancel.
type releasingStagedWriter struct {
	StagedWriter
	release func()
}

func (w *releasingStagedWriter) Commit(ctx context.Context, path, key string) error {
	defer w.release()
	return w.StagedWriter.Commit(ctx, path, key)
}

func (w *releasingStagedWriter) Cancel(ctx context.Context) error {
	defer w.release()
	return w.StagedWriter.Cancel(ctx)
}
