package backends

import (
	"bytes"
	"context"
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
		max := f.maxSeen.Load()
		if n <= max || f.maxSeen.CompareAndSwap(max, n) {
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
func withGlobalIOConcurrency(t *testing.T, cap int) {
	t.Helper()
	prev := globalIOSem.Load()
	t.Cleanup(func() {
		globalIOSemMu.Lock()
		defer globalIOSemMu.Unlock()
		globalIOSem.Store(prev)
	})
	SetGlobalIOConcurrency(cap)
}

func TestBoundedBackend_BoundsConcurrentSets(t *testing.T) {
	const limit = 4
	const callers = 32

	withGlobalIOConcurrency(t, limit)

	fake := &fakeBackend{enterDur: 5 * time.Millisecond}
	bb := NewBoundedBackend(fake)

	var wg sync.WaitGroup
	wg.Add(callers)
	for i := 0; i < callers; i++ {
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
	for i := 0; i < limit; i++ {
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
	for i := 0; i < sessions; i++ {
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

// TestAcquireGlobalIO_PreventsDoubleCount asserts that a backend call made
// with a context from AcquireGlobalIO does not consume a second slot — the
// property uploadFilesToCas relies on to avoid deadlock.
func TestAcquireGlobalIO_PreventsDoubleCount(t *testing.T) {
	const limit = 1
	withGlobalIOConcurrency(t, limit)

	fake := &fakeBackend{}
	bb := NewBoundedBackend(fake)

	ctx, release, err := AcquireGlobalIO(context.Background())
	if err != nil {
		t.Fatalf("AcquireGlobalIO: %v", err)
	}
	defer release()

	// With a 1-slot cap and the slot already held, a naive double-acquire
	// would deadlock; the preacquired marker must short-circuit it.
	done := make(chan error, 1)
	go func() {
		done <- bb.Set(ctx, "p", "k", strings.NewReader("x"))
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Set: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Set deadlocked despite preacquired ctx")
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
	for i := 0; i < callers; i++ {
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
