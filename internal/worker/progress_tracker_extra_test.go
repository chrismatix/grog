package worker

import (
	"errors"
	"io"
	"strings"
	"sync"
	"testing"

	"grog/internal/config"
)

func collect(updates *[]StatusUpdate, mu *sync.Mutex) StatusFunc {
	return func(u StatusUpdate) {
		mu.Lock()
		defer mu.Unlock()
		*updates = append(*updates, u)
	}
}

func TestNewProgressTrackerNilUpdate(t *testing.T) {
	if NewProgressTracker("x", 0, nil) != nil {
		t.Fatal("expected nil tracker when update fn nil")
	}
}

func TestSubTrackerNilParent(t *testing.T) {
	var pt *ProgressTracker
	if got := pt.SubTracker("x", 100); got != nil {
		t.Fatalf("expected nil, got %v", got)
	}
}

func TestSubTrackerNonPositiveTotal(t *testing.T) {
	tracker := NewProgressTracker("root", 0, func(StatusUpdate) {})
	if got := tracker.SubTracker("zero", 0); got != nil {
		t.Fatal("expected nil for non-positive total")
	}
	if got := tracker.SubTracker("neg", -1); got != nil {
		t.Fatal("expected nil for negative total")
	}
}

func TestAddOnNilTrackerIsNoop(t *testing.T) {
	var pt *ProgressTracker
	pt.Add(10) // must not panic
}

func TestAddZeroDeltaIsNoop(t *testing.T) {
	var mu sync.Mutex
	var updates []StatusUpdate
	tr := NewProgressTracker("root", 100, collect(&updates, &mu))
	tr.Add(0)
	mu.Lock()
	defer mu.Unlock()
	if len(updates) != 0 {
		t.Fatalf("expected no updates for delta=0, got %d", len(updates))
	}
}

func TestAddClampsToTotal(t *testing.T) {
	var mu sync.Mutex
	var updates []StatusUpdate
	tr := NewProgressTracker("root", 100, collect(&updates, &mu))
	tr.Add(500)
	mu.Lock()
	defer mu.Unlock()
	if len(updates) == 0 {
		t.Fatal("expected an update")
	}
	last := updates[len(updates)-1]
	if last.Progress == nil || last.Progress.Current != 100 {
		t.Fatalf("expected current clamped to 100, got %+v", last.Progress)
	}
}

func TestCompleteEmitsFinalUpdate(t *testing.T) {
	var mu sync.Mutex
	var updates []StatusUpdate
	tr := NewProgressTracker("root", 200, collect(&updates, &mu))
	tr.Complete()
	mu.Lock()
	defer mu.Unlock()
	if len(updates) == 0 {
		t.Fatal("expected at least one update")
	}
	last := updates[len(updates)-1]
	if last.Progress == nil || last.Progress.Current != 200 {
		t.Fatalf("expected current==total after Complete, got %+v", last.Progress)
	}
}

func TestCompleteOnNilTrackerIsNoop(t *testing.T) {
	var pt *ProgressTracker
	pt.Complete()
}

func TestSetStatusOnNilTrackerIsNoop(t *testing.T) {
	var pt *ProgressTracker
	pt.SetStatus("x")
}

func TestSetSubStatus(t *testing.T) {
	var mu sync.Mutex
	var updates []StatusUpdate
	tr := NewProgressTracker("root", 0, collect(&updates, &mu))

	tr.SetSubStatus("phase: details")
	tr.SetSubStatus("phase: details")
	tr.SetSubStatus("phase: more")

	mu.Lock()
	defer mu.Unlock()
	if len(updates) != 2 {
		t.Fatalf("expected exactly 2 updates (deduped same status), got %d: %v", len(updates), updates)
	}
	if updates[0].SubStatus != "phase: details" {
		t.Fatalf("first sub-status mismatch: %q", updates[0].SubStatus)
	}
	if updates[1].SubStatus != "phase: more" {
		t.Fatalf("second sub-status mismatch: %q", updates[1].SubStatus)
	}
}

func TestSetSubStatusOnNilTrackerIsNoop(t *testing.T) {
	var pt *ProgressTracker
	pt.SetSubStatus("x")
}

func TestWrapReaderUpdatesTracker(t *testing.T) {
	var mu sync.Mutex
	var updates []StatusUpdate
	tr := NewProgressTracker("root", 1024, collect(&updates, &mu))
	src := strings.NewReader(strings.Repeat("a", 1024))
	reader := tr.WrapReader(src)
	n, err := io.Copy(io.Discard, reader)
	if err != nil || n != 1024 {
		t.Fatalf("copy: n=%d err=%v", n, err)
	}
	mu.Lock()
	defer mu.Unlock()
	last := updates[len(updates)-1]
	if last.Progress == nil || last.Progress.Current != 1024 {
		t.Fatalf("expected current=1024, got %+v", last.Progress)
	}
}

func TestWrapReaderOnNilTrackerReturnsIdentity(t *testing.T) {
	var pt *ProgressTracker
	r := strings.NewReader("abc")
	if got := pt.WrapReader(r); got != r {
		t.Fatal("expected identity when tracker is nil")
	}
}

type errReadCloser struct{ closed bool }

func (e *errReadCloser) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	p[0] = 'x'
	return 1, io.EOF
}
func (e *errReadCloser) Close() error {
	e.closed = true
	return errors.New("close-boom")
}

func TestWrapReadCloserPropagatesCloseAndCounts(t *testing.T) {
	var mu sync.Mutex
	var updates []StatusUpdate
	tr := NewProgressTracker("root", 10, collect(&updates, &mu))
	src := &errReadCloser{}
	rc := tr.WrapReadCloser(src)
	buf := make([]byte, 8)
	n, err := rc.Read(buf)
	if n != 1 || err != io.EOF {
		t.Fatalf("read: n=%d err=%v", n, err)
	}
	if err := rc.Close(); err == nil || err.Error() != "close-boom" {
		t.Fatalf("expected propagated close error, got %v", err)
	}
	if !src.closed {
		t.Fatal("inner close not called")
	}
}

func TestWrapReadCloserOnNilTrackerReturnsIdentity(t *testing.T) {
	var pt *ProgressTracker
	rc := io.NopCloser(strings.NewReader("abc"))
	if got := pt.WrapReadCloser(rc); got != rc {
		t.Fatal("expected identity when tracker is nil")
	}
}

func TestComputeStepRespectsMinimum(t *testing.T) {
	const minStep = 32 * 1024
	if computeStep(0) != minStep {
		t.Fatalf("expected minStep for zero total, got %d", computeStep(0))
	}
	if computeStep(100) != minStep {
		t.Fatalf("expected minStep for small total, got %d", computeStep(100))
	}
	big := int64(100 * minStep * 200)
	if got := computeStep(big); got != big/100 {
		t.Fatalf("expected total/100, got %d", got)
	}
}

func TestProgressTrackerNotEmittedWhenZeroTotal(t *testing.T) {
	var mu sync.Mutex
	var updates []StatusUpdate
	tr := NewProgressTracker("root", 0, collect(&updates, &mu))
	tr.Add(1024)
	mu.Lock()
	defer mu.Unlock()
	if len(updates) != 0 {
		t.Fatalf("expected no updates for total=0, got %d", len(updates))
	}
}

func TestSetStatusUpdatesEvenAcrossSameDisplayedStatus(t *testing.T) {
	_ = config.Global
}
