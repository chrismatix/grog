package backends

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakeBackend is a minimal CacheBackend that records peak in-flight
// concurrency so tests can observe what the wrapper bounds.
type fakeBackend struct {
	inFlight  atomic.Int32
	maxSeen   atomic.Int32
	enterDur  time.Duration
	beginDur  time.Duration
	commitDur time.Duration
}

func (f *fakeBackend) recordEntry() {
	n := f.inFlight.Add(1)
	for {
		maximum := f.maxSeen.Load()
		if n <= maximum || f.maxSeen.CompareAndSwap(maximum, n) {
			break
		}
	}
}

func (f *fakeBackend) recordExit() { f.inFlight.Add(-1) }

func (f *fakeBackend) TypeName() string { return "fake" }

func (f *fakeBackend) Get(ctx context.Context, path, key string) (io.ReadCloser, error) {
	f.recordEntry()
	defer f.recordExit()
	time.Sleep(f.enterDur)
	return io.NopCloser(strings.NewReader("ok")), nil
}

func (f *fakeBackend) Set(ctx context.Context, path, key string, content io.Reader) error {
	f.recordEntry()
	defer f.recordExit()
	_, _ = io.Copy(io.Discard, content)
	time.Sleep(f.enterDur)
	return nil
}

func (f *fakeBackend) BeginWrite(ctx context.Context) (StagedWriter, error) {
	f.recordEntry()
	time.Sleep(f.beginDur)
	return &fakeStagedWriter{parent: f}, nil
}

func (f *fakeBackend) Delete(ctx context.Context, path, key string) error { return nil }
func (f *fakeBackend) Exists(ctx context.Context, path, key string) (bool, error) {
	return false, nil
}
func (f *fakeBackend) Size(ctx context.Context, path, key string) (int64, error) { return 0, nil }
func (f *fakeBackend) ListKeys(ctx context.Context, path, suffix string) ([]string, error) {
	return nil, nil
}

type fakeStagedWriter struct {
	parent *fakeBackend
	buf    bytes.Buffer
}

func (w *fakeStagedWriter) Write(p []byte) (int, error) { return w.buf.Write(p) }
func (w *fakeStagedWriter) Commit(ctx context.Context, path, key string) error {
	defer w.parent.recordExit()
	time.Sleep(w.parent.commitDur)
	return nil
}
func (w *fakeStagedWriter) Cancel(ctx context.Context) error {
	w.parent.recordExit()
	return nil
}

// withGlobalIOConcurrency installs a temporary cap, restored at test end.
func withGlobalIOConcurrency(t *testing.T, capacity int) {
	t.Helper()
	prevSem := globalIOSem.Load()
	prevCap := globalIOCap.Load()
	t.Cleanup(func() {
		globalIOSemMu.Lock()
		defer globalIOSemMu.Unlock()
		globalIOSem.Store(prevSem)
		globalIOCap.Store(prevCap)
	})
	SetGlobalIOConcurrency(capacity)
}

func TestBoundedBackend_BoundsConcurrentSets(t *testing.T) {
	const limit = 4
	const callers = 32

	withGlobalIOConcurrency(t, limit)

	fake := &fakeBackend{enterDur: 5 * time.Millisecond}
	bb := NewBoundedBackend(fake)

	var wg sync.WaitGroup
	wg.Add(callers)
	for range callers {
		go func() {
			defer wg.Done()
			if err := bb.Set(context.Background(), "p", "k", strings.NewReader("x")); err != nil {
				t.Errorf("Set failed: %v", err)
			}
		}()
	}
	wg.Wait()

	if got := fake.maxSeen.Load(); got > int32(limit) {
		t.Fatalf("max in-flight sets = %d, want <= %d", got, limit)
	}
	if got := fake.maxSeen.Load(); got == 0 {
		t.Fatalf("expected at least one in-flight set, got 0")
	}
}

func TestBoundedBackend_GetReleasesOnReaderClose(t *testing.T) {
	const limit = 2
	withGlobalIOConcurrency(t, limit)

	fake := &fakeBackend{}
	bb := NewBoundedBackend(fake)

	// Acquire `limit` slots and hold them via open readers.
	readers := make([]io.ReadCloser, 0, limit)
	for i := range limit {
		rc, err := bb.Get(context.Background(), "p", "k")
		if err != nil {
			t.Fatalf("Get %d failed: %v", i, err)
		}
		readers = append(readers, rc)
	}

	// A further Get must block until a reader is closed.
	done := make(chan struct{})
	go func() {
		rc, err := bb.Get(context.Background(), "p", "k")
		if err == nil {
			rc.Close()
		}
		close(done)
	}()

	select {
	case <-done:
		t.Fatal("Get returned while all slots were held by open readers")
	case <-time.After(50 * time.Millisecond):
	}

	// Closing one reader should free a slot and unblock the pending Get.
	if err := readers[0].Close(); err != nil {
		t.Fatalf("close reader: %v", err)
	}

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Get did not unblock after reader close")
	}

	for _, rc := range readers[1:] {
		_ = rc.Close()
	}
}

// TestBoundedBackend_BeginWriteIsNotBounded documents the carve-out:
// staged writes bypass the global I/O semaphore, since slots would
// otherwise leak across the long lifetime of an interrupted upload.
func TestBoundedBackend_BeginWriteIsNotBounded(t *testing.T) {
	const limit = 1
	withGlobalIOConcurrency(t, limit)

	fake := &fakeBackend{}
	bb := NewBoundedBackend(fake)

	const sessions = 8
	writers := make([]StagedWriter, 0, sessions)
	for i := range sessions {
		sw, err := bb.BeginWrite(context.Background())
		if err != nil {
			t.Fatalf("BeginWrite %d: %v", i, err)
		}
		writers = append(writers, sw)
	}

	for _, sw := range writers {
		if err := sw.Cancel(context.Background()); err != nil {
			t.Fatalf("Cancel: %v", err)
		}
	}
}

// TestGlobalIOConcurrency_ReportsConfiguredCap asserts the accessor that
// callers fanning out their own goroutines (e.g. directory uploads) use to
// size their concurrency: it returns the configured cap, and falls back to
// the default when the limit is disabled.
func TestGlobalIOConcurrency_ReportsConfiguredCap(t *testing.T) {
	const limit = 7
	withGlobalIOConcurrency(t, limit)
	if got := GlobalIOConcurrency(); got != limit {
		t.Fatalf("GlobalIOConcurrency() = %d, want %d", got, limit)
	}

	withGlobalIOConcurrency(t, 0) // disabled
	if got := GlobalIOConcurrency(); got != DefaultIOConcurrency() {
		t.Fatalf("GlobalIOConcurrency() with limit disabled = %d, want default %d",
			got, DefaultIOConcurrency())
	}
}

func TestBoundedBackend_PassthroughWhenSemDisabled(t *testing.T) {
	withGlobalIOConcurrency(t, 0) // disabled

	// Hold each Set in flight long enough for goroutines to overlap;
	// without a sleep the scheduler may serialise short calls and hide
	// the absence of bounding.
	fake := &fakeBackend{enterDur: 10 * time.Millisecond}
	bb := NewBoundedBackend(fake)

	const callers = 16
	var wg sync.WaitGroup
	wg.Add(callers)
	for range callers {
		go func() {
			defer wg.Done()
			_ = bb.Set(context.Background(), "p", "k", strings.NewReader("x"))
		}()
	}
	wg.Wait()

	if got := fake.maxSeen.Load(); got < 2 {
		t.Fatalf("expected unbounded concurrency, max in-flight = %d", got)
	}
}

func TestDefaultIOConcurrency_ClampedRange(t *testing.T) {
	got := DefaultIOConcurrency()
	if got < defaultIOConcurrencyMin || got > defaultIOConcurrencyMax {
		t.Fatalf("DefaultIOConcurrency() = %d, want in [%d, %d]",
			got, defaultIOConcurrencyMin, defaultIOConcurrencyMax)
	}
}

func TestBoundedBackend_InnerAndTypeName(t *testing.T) {
	withGlobalIOConcurrency(t, 4)
	inner := &mockCacheBackend{typeName: "inner-name"}
	bb := NewBoundedBackend(inner)
	if got := bb.Inner(); got != inner {
		t.Fatalf("Inner() = %v, want %v", got, inner)
	}
	if got := bb.TypeName(); got != "inner-name" {
		t.Fatalf("TypeName() = %q, want inner-name", got)
	}
}

func TestBoundedBackend_DelegationCalls(t *testing.T) {
	withGlobalIOConcurrency(t, 4)
	ctx := context.Background()

	deleteCalled := false
	existsCalled := false
	sizeCalled := false
	listCalled := false

	inner := &mockCacheBackend{
		deleteFunc: func(ctx context.Context, path, key string) error {
			deleteCalled = true
			return nil
		},
		existsFunc: func(ctx context.Context, path, key string) (bool, error) {
			existsCalled = true
			return true, nil
		},
		sizeFunc: func(ctx context.Context, path, key string) (int64, error) {
			sizeCalled = true
			return 42, nil
		},
	}

	listingMock := &listingMockBackend{
		mockCacheBackend: inner,
		listFunc: func(ctx context.Context, path, suffix string) ([]string, error) {
			listCalled = true
			return []string{"a", "b"}, nil
		},
	}

	bb := NewBoundedBackend(listingMock)

	if err := bb.Delete(ctx, "p", "k"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if !deleteCalled {
		t.Fatal("Delete not delegated")
	}

	exists, err := bb.Exists(ctx, "p", "k")
	if err != nil || !exists {
		t.Fatalf("Exists: %v, %v", exists, err)
	}
	if !existsCalled {
		t.Fatal("Exists not delegated")
	}

	size, err := bb.Size(ctx, "p", "k")
	if err != nil || size != 42 {
		t.Fatalf("Size: %d, %v", size, err)
	}
	if !sizeCalled {
		t.Fatal("Size not delegated")
	}

	keys, err := bb.ListKeys(ctx, "p", "")
	if err != nil || len(keys) != 2 {
		t.Fatalf("ListKeys: %v, %v", keys, err)
	}
	if !listCalled {
		t.Fatal("ListKeys not delegated")
	}
}

func TestBoundedBackend_DelegationPropagatesErrors(t *testing.T) {
	withGlobalIOConcurrency(t, 4)
	ctx := context.Background()
	wantErr := errors.New("inner failure")

	inner := &mockCacheBackend{
		deleteFunc: func(ctx context.Context, path, key string) error { return wantErr },
		existsFunc: func(ctx context.Context, path, key string) (bool, error) {
			return false, wantErr
		},
		sizeFunc: func(ctx context.Context, path, key string) (int64, error) {
			return 0, wantErr
		},
	}
	listingMock := &listingMockBackend{
		mockCacheBackend: inner,
		listFunc: func(ctx context.Context, path, suffix string) ([]string, error) {
			return nil, wantErr
		},
	}
	bb := NewBoundedBackend(listingMock)

	if err := bb.Delete(ctx, "p", "k"); !errors.Is(err, wantErr) {
		t.Fatalf("Delete err = %v", err)
	}
	if _, err := bb.Exists(ctx, "p", "k"); !errors.Is(err, wantErr) {
		t.Fatalf("Exists err = %v", err)
	}
	if _, err := bb.Size(ctx, "p", "k"); !errors.Is(err, wantErr) {
		t.Fatalf("Size err = %v", err)
	}
	if _, err := bb.ListKeys(ctx, "p", ""); !errors.Is(err, wantErr) {
		t.Fatalf("ListKeys err = %v", err)
	}
}

func TestBoundedBackend_ContextCancelledOnAcquire(t *testing.T) {
	withGlobalIOConcurrency(t, 1)

	inner := &mockCacheBackend{
		deleteFunc: func(ctx context.Context, path, key string) error { return nil },
		existsFunc: func(ctx context.Context, path, key string) (bool, error) {
			return false, nil
		},
		sizeFunc: func(ctx context.Context, path, key string) (int64, error) { return 0, nil },
		setFunc: func(ctx context.Context, path, key string, content io.Reader) error {
			return nil
		},
		getFunc: func(ctx context.Context, path, key string) (io.ReadCloser, error) {
			return io.NopCloser(strings.NewReader("")), nil
		},
	}
	listingMock := &listingMockBackend{
		mockCacheBackend: inner,
		listFunc: func(ctx context.Context, path, suffix string) ([]string, error) {
			return nil, nil
		},
	}
	bb := NewBoundedBackend(listingMock)

	rc, err := bb.Get(context.Background(), "p", "k")
	if err != nil {
		t.Fatalf("prime acquire: %v", err)
	}
	defer rc.Close()

	cancelCtx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := bb.Get(cancelCtx, "p", "k"); err == nil {
		t.Fatal("Get with cancelled ctx should fail")
	}
	if err := bb.Set(cancelCtx, "p", "k", strings.NewReader("x")); err == nil {
		t.Fatal("Set with cancelled ctx should fail")
	}
	if err := bb.Delete(cancelCtx, "p", "k"); err == nil {
		t.Fatal("Delete with cancelled ctx should fail")
	}
	if _, err := bb.Exists(cancelCtx, "p", "k"); err == nil {
		t.Fatal("Exists with cancelled ctx should fail")
	}
	if _, err := bb.Size(cancelCtx, "p", "k"); err == nil {
		t.Fatal("Size with cancelled ctx should fail")
	}
	if _, err := bb.ListKeys(cancelCtx, "p", ""); err == nil {
		t.Fatal("ListKeys with cancelled ctx should fail")
	}
}

func TestBoundedBackend_GetReleasesOnInnerError(t *testing.T) {
	withGlobalIOConcurrency(t, 1)
	inner := &mockCacheBackend{
		getFunc: func(ctx context.Context, path, key string) (io.ReadCloser, error) {
			return nil, errors.New("inner get fail")
		},
	}
	bb := NewBoundedBackend(inner)

	if _, err := bb.Get(context.Background(), "p", "k"); err == nil {
		t.Fatal("expected error")
	}
	if _, err := bb.Get(context.Background(), "p", "k"); err == nil {
		t.Fatal("expected error")
	}
}

type listingMockBackend struct {
	*mockCacheBackend
	listFunc func(ctx context.Context, path, suffix string) ([]string, error)
}

func (m *listingMockBackend) ListKeys(ctx context.Context, path, suffix string) ([]string, error) {
	if m.listFunc != nil {
		return m.listFunc(ctx, path, suffix)
	}
	return nil, nil
}
