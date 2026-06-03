package handlers

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"grog/internal/label"
	"grog/internal/model"
	"grog/internal/proto/gen"
	"grog/internal/worker"
)

// fakeInnerHandler stands in for DockerOutputHandler / DockerRegistryOutputHandler
// in OciPushHandler tests. It records the calls it sees, returns a canned
// DockerImage proto from Write, and implements DockerImageSource so the push
// plan can resolve a source ref without a real registry.
type fakeInnerHandler struct {
	writeCalls atomic.Int32
	hashCalls  atomic.Int32
	loadCalls  atomic.Int32

	imageID     string
	sourceRef   string
	sourceOk    bool
	innerPlan   OutputWritePlan
	failOnWrite error
}

func (f *fakeInnerHandler) Type() HandlerType { return DockerHandler }

func (f *fakeInnerHandler) Hash(_ context.Context, _ model.Target, _ model.Output) (string, error) {
	f.hashCalls.Add(1)
	return f.imageID, nil
}

func (f *fakeInnerHandler) Write(_ context.Context, _ model.Target, out model.Output, _ *worker.ProgressTracker) (*PreparedOutput, error) {
	f.writeCalls.Add(1)
	if f.failOnWrite != nil {
		return nil, f.failOnWrite
	}
	img := &gen.DockerImageOutput{
		LocalTag: out.Identifier,
		ImageId:  f.imageID,
	}
	return &PreparedOutput{
		Output:    &gen.Output{Kind: &gen.Output_DockerImage{DockerImage: img}},
		WritePlan: f.innerPlan,
	}, nil
}

func (f *fakeInnerHandler) Load(_ context.Context, _ model.Target, _ *gen.Output, _ *worker.ProgressTracker) error {
	f.loadCalls.Add(1)
	return nil
}

func (f *fakeInnerHandler) SourceRef(_ *gen.DockerImageOutput) (string, bool, bool) {
	return f.sourceRef, false, f.sourceOk
}

// recordingPushFn captures the args the push plan would have sent to a real
// registry copier. Returns whatever stub (skipped, error) the test sets.
type recordingPushFn struct {
	calls   []pushCall
	skipped bool
	err     error
}

type pushCall struct {
	source, destination string
	insecure            bool
}

func (r *recordingPushFn) fn() PushFunc {
	return func(_ context.Context, src, dst string, insecure bool) (bool, error) {
		r.calls = append(r.calls, pushCall{source: src, destination: dst, insecure: insecure})
		return r.skipped, r.err
	}
}

func newTargetWithLabel(t *testing.T, lbl string) model.Target {
	t.Helper()
	pkg, name, ok := splitLabel(lbl)
	if !ok {
		t.Fatalf("invalid test label %q", lbl)
	}
	return model.Target{Label: label.TL(pkg, name)}
}

func splitLabel(s string) (string, string, bool) {
	// Bare helper; the real parser is overkill for these test labels.
	const sep = ":"
	for i := len(s) - 1; i >= 0; i-- {
		if string(s[i]) == sep {
			return s[:i], s[i+1:], true
		}
	}
	return "", "", false
}

func TestOciPushHandler_PushDisabled_NoPushPlan(t *testing.T) {
	inner := &fakeInnerHandler{imageID: "sha256:abc"}
	pusher := &recordingPushFn{}
	h := NewOciPushOutputHandler(inner, pusher.fn(), NewPushReporter(),
		func() bool { return false }, // push disabled
		func() bool { return false },
	)

	target := newTargetWithLabel(t, "//pkg:tgt")
	prepared, err := h.Write(context.Background(), target,
		model.NewOutput(string(OciPushHandler), "repo/image:1.2.3"),
		newProgress(t))
	if err != nil {
		t.Fatalf("Write returned error: %v", err)
	}

	// push_destination should still be stamped so cache round-trips it.
	if got := prepared.Output.GetDockerImage().GetPushDestination(); got != "repo/image:1.2.3" {
		t.Errorf("push_destination = %q, want %q", got, "repo/image:1.2.3")
	}

	// With push disabled, the inner plan flows through unwrapped — no
	// CompositeWritePlan wrapper.
	if _, isComposite := prepared.WritePlan.(*CompositeWritePlan); isComposite {
		t.Errorf("expected raw inner plan when --push is off, got composite")
	}
	if len(pusher.calls) != 0 {
		t.Errorf("push function was called %d times despite --push being off", len(pusher.calls))
	}
}

func TestOciPushHandler_PushEnabled_AppendsPushPlan(t *testing.T) {
	inner := &fakeInnerHandler{
		imageID:   "sha256:abc",
		sourceRef: "cache.example.com/grog/abc",
		sourceOk:  true,
		innerPlan: NewNopWritePlan(),
	}
	pusher := &recordingPushFn{}
	reporter := NewPushReporter()
	h := NewOciPushOutputHandler(inner, pusher.fn(), reporter,
		func() bool { return true },
		func() bool { return false },
	)

	target := newTargetWithLabel(t, "//pkg:tgt")
	prepared, err := h.Write(context.Background(), target,
		model.NewOutput(string(OciPushHandler), "repo/image:1.2.3"),
		newProgress(t))
	if err != nil {
		t.Fatalf("Write returned error: %v", err)
	}

	composite, ok := prepared.WritePlan.(*CompositeWritePlan)
	if !ok {
		t.Fatalf("expected CompositeWritePlan, got %T", prepared.WritePlan)
	}
	if len(composite.Plans) != 2 {
		t.Fatalf("composite has %d plans, want 2", len(composite.Plans))
	}

	if err := composite.Execute(context.Background(), newProgress(t)); err != nil {
		t.Fatalf("composite.Execute: %v", err)
	}

	if len(pusher.calls) != 1 {
		t.Fatalf("push called %d times, want 1", len(pusher.calls))
	}
	call := pusher.calls[0]
	if call.source != "cache.example.com/grog/abc" || call.destination != "repo/image:1.2.3" {
		t.Errorf("push args = (%s -> %s), want (cache.example.com/grog/abc -> repo/image:1.2.3)", call.source, call.destination)
	}

	reports := reporter.Reports()
	if len(reports) != 1 || reports[0].Err != nil {
		t.Errorf("reports = %+v, want one success", reports)
	}
}

func TestOciPushHandler_PushFailure_RecordsAndPropagates(t *testing.T) {
	inner := &fakeInnerHandler{
		imageID:   "sha256:abc",
		sourceRef: "cache/abc",
		sourceOk:  true,
		innerPlan: NewNopWritePlan(),
	}
	pushErr := errors.New("simulated 503")
	pusher := &recordingPushFn{err: pushErr}
	reporter := NewPushReporter()
	h := NewOciPushOutputHandler(inner, pusher.fn(), reporter,
		func() bool { return true },
		func() bool { return false },
	)

	target := newTargetWithLabel(t, "//pkg:tgt")
	prepared, err := h.Write(context.Background(), target,
		model.NewOutput(string(OciPushHandler), "repo/image:1.2.3"),
		newProgress(t))
	if err != nil {
		t.Fatalf("Write: %v", err)
	}

	execErr := prepared.WritePlan.Execute(context.Background(), newProgress(t))
	if !errors.Is(execErr, pushErr) {
		t.Errorf("Execute err = %v, want wrap of %v", execErr, pushErr)
	}
	if !reporter.HasFailures() {
		t.Errorf("expected reporter to record failure")
	}
}

func TestOciPushHandler_FailFast_AbortsSubsequent(t *testing.T) {
	inner := &fakeInnerHandler{
		imageID:   "sha256:abc",
		sourceRef: "cache/abc",
		sourceOk:  true,
		innerPlan: NewNopWritePlan(),
	}
	pushErr := errors.New("first failure")
	pusher := &recordingPushFn{err: pushErr}
	reporter := NewPushReporter()
	h := NewOciPushOutputHandler(inner, pusher.fn(), reporter,
		func() bool { return true },
		func() bool { return true }, // fail-fast on
	)

	target := newTargetWithLabel(t, "//pkg:tgt")
	// Build two push plans from the same handler so they share the abort
	// flag — simulating two targets pushing concurrently.
	out1 := model.NewOutput(string(OciPushHandler), "repo/a:1")
	out2 := model.NewOutput(string(OciPushHandler), "repo/b:1")
	p1, _ := h.Write(context.Background(), target, out1, newProgress(t))
	p2, _ := h.Write(context.Background(), target, out2, newProgress(t))

	if err := p1.WritePlan.Execute(context.Background(), newProgress(t)); err == nil {
		t.Fatalf("expected first push to fail")
	}
	// Second plan should short-circuit without calling pushFn again.
	beforeCalls := len(pusher.calls)
	if err := p2.WritePlan.Execute(context.Background(), newProgress(t)); err == nil {
		t.Fatalf("expected second push to abort")
	}
	if len(pusher.calls) != beforeCalls {
		t.Errorf("pushFn was invoked after abort (was %d, now %d)", beforeCalls, len(pusher.calls))
	}
}

func TestOciPushHandler_Load_PushEnabled_PushesAfterRestore(t *testing.T) {
	// Simulates the cache-hit code path: the inner Load restores the image
	// from cache, then the push fires against the destination. This is what
	// makes "redeploy unchanged image" cheap — no rebuild, just a HEAD probe
	// + tag retag.
	inner := &fakeInnerHandler{
		imageID:   "sha256:abc",
		sourceRef: "cache.example.com/grog/abc",
		sourceOk:  true,
	}
	pusher := &recordingPushFn{skipped: true} // destination already current
	reporter := NewPushReporter()
	h := NewOciPushOutputHandler(inner, pusher.fn(), reporter,
		func() bool { return true },
		func() bool { return false },
	)

	target := newTargetWithLabel(t, "//pkg:tgt")
	cachedOutput := &gen.Output{
		Kind: &gen.Output_DockerImage{
			DockerImage: &gen.DockerImageOutput{
				LocalTag:        "repo/image:1.2.3",
				ImageId:         "sha256:abc",
				PushDestination: "repo/image:1.2.3",
			},
		},
	}

	if err := h.Load(context.Background(), target, cachedOutput, newProgress(t)); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if inner.loadCalls.Load() != 1 {
		t.Errorf("inner Load called %d times, want 1", inner.loadCalls.Load())
	}
	if len(pusher.calls) != 1 {
		t.Fatalf("push called %d times, want 1", len(pusher.calls))
	}
	reports := reporter.Reports()
	if len(reports) != 1 || !reports[0].Skipped || reports[0].Err != nil {
		t.Errorf("expected one skipped report, got %+v", reports)
	}
}

func TestOciPushHandler_Load_PushFailure_DoesNotPropagate(t *testing.T) {
	// A push failure during Load must NOT force a rebuild — return nil to
	// the executor so it does not interpret this as "loading failed."
	inner := &fakeInnerHandler{imageID: "sha256:abc", sourceRef: "cache/abc", sourceOk: true}
	pusher := &recordingPushFn{err: errors.New("registry down")}
	reporter := NewPushReporter()
	h := NewOciPushOutputHandler(inner, pusher.fn(), reporter,
		func() bool { return true },
		func() bool { return false },
	)

	target := newTargetWithLabel(t, "//pkg:tgt")
	out := &gen.Output{
		Kind: &gen.Output_DockerImage{
			DockerImage: &gen.DockerImageOutput{
				LocalTag: "x", ImageId: "sha256:abc", PushDestination: "repo/x:1",
			},
		},
	}
	if err := h.Load(context.Background(), target, out, newProgress(t)); err != nil {
		t.Fatalf("Load returned %v; push failures must not propagate from Load", err)
	}
	if !reporter.HasFailures() {
		t.Errorf("reporter should still see the failure for the build-summary exit code")
	}
}

// newProgress builds a minimal ProgressTracker for handler tests. The tracker
// drains all updates into a no-op StatusFunc.
func newProgress(t *testing.T) *worker.ProgressTracker {
	t.Helper()
	return worker.NewProgressTracker("test", 0, func(worker.StatusUpdate) {})
}
