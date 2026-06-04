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

// fakeInnerHandler stands in for a docker output backend in OciPushOutputHandler
// tests. It records call counts and returns a canned image proto, and
// implements ImagePusher so the wrapper's type-assertion succeeds.
type fakeInnerHandler struct {
	writeCalls atomic.Int32
	loadCalls  atomic.Int32
	pushCalls  []pushCall
	pushRes    pushResult

	imageID   string
	innerPlan OutputWritePlan
}

type pushCall struct {
	destination string
	imageID     string
}

type pushResult struct {
	skipped bool
	err     error
}

func (f *fakeInnerHandler) Type() HandlerType { return OCIHandler }

func (f *fakeInnerHandler) Hash(_ context.Context, _ model.Target, _ model.Output) (string, error) {
	return f.imageID, nil
}

func (f *fakeInnerHandler) Write(_ context.Context, _ model.Target, out model.Output, _ *worker.ProgressTracker) (*PreparedOutput, error) {
	f.writeCalls.Add(1)
	img := &gen.OCIImageOutput{LocalTag: out.Identifier, ImageId: f.imageID}
	return &PreparedOutput{
		Output:    &gen.Output{Kind: &gen.Output_OciImage{OciImage: img}},
		WritePlan: f.innerPlan,
	}, nil
}

func (f *fakeInnerHandler) Load(_ context.Context, _ model.Target, _ *gen.Output, _ *worker.ProgressTracker) error {
	f.loadCalls.Add(1)
	return nil
}

func (f *fakeInnerHandler) PushImage(_ context.Context, image *gen.OCIImageOutput, destination string, _ *worker.ProgressTracker) (bool, error) {
	f.pushCalls = append(f.pushCalls, pushCall{destination: destination, imageID: image.GetImageId()})
	return f.pushRes.skipped, f.pushRes.err
}

func newTestTarget(t *testing.T, name string) model.Target {
	t.Helper()
	return model.Target{Label: label.TL("pkg", name)}
}

func newTestProgress(t *testing.T) *worker.ProgressTracker {
	t.Helper()
	return worker.NewProgressTracker("test", 0, func(worker.StatusUpdate) {})
}

func TestOciPushOutputHandler_PushDisabled_DelegatesOnly(t *testing.T) {
	inner := &fakeInnerHandler{imageID: "sha256:abc", innerPlan: NewNopWritePlan()}
	h := NewOciPushOutputHandler(inner, NewPushReporter(nil), func() bool { return false })

	prepared, err := h.Write(context.Background(), newTestTarget(t, "tgt"),
		model.NewOutput(string(OciPushHandler), "repo/image:1"),
		newTestProgress(t))
	if err != nil {
		t.Fatalf("Write: %v", err)
	}

	if got := prepared.Output.GetOciImage().GetPushDestination(); got != "repo/image:1" {
		t.Errorf("push_destination = %q, want %q", got, "repo/image:1")
	}
	if _, isComposite := prepared.WritePlan.(*CompositeWritePlan); isComposite {
		t.Errorf("expected raw inner plan when --push is off, got composite")
	}
	if len(inner.pushCalls) != 0 {
		t.Errorf("push was called %d times despite --push being off", len(inner.pushCalls))
	}
}

func TestOciPushOutputHandler_PushEnabled_ChainsPushPlan(t *testing.T) {
	inner := &fakeInnerHandler{imageID: "sha256:abc", innerPlan: NewNopWritePlan()}
	reporter := NewPushReporter(nil)
	h := NewOciPushOutputHandler(inner, reporter, func() bool { return true })

	prepared, err := h.Write(context.Background(), newTestTarget(t, "tgt"),
		model.NewOutput(string(OciPushHandler), "repo/image:1"),
		newTestProgress(t))
	if err != nil {
		t.Fatalf("Write: %v", err)
	}

	composite, ok := prepared.WritePlan.(*CompositeWritePlan)
	if !ok {
		t.Fatalf("expected CompositeWritePlan, got %T", prepared.WritePlan)
	}
	if len(composite.Plans) != 2 {
		t.Fatalf("composite has %d plans, want 2", len(composite.Plans))
	}
	if err := composite.Execute(context.Background(), newTestProgress(t)); err != nil {
		t.Fatalf("composite.Execute: %v", err)
	}
	if len(inner.pushCalls) != 1 || inner.pushCalls[0].destination != "repo/image:1" {
		t.Errorf("push calls = %+v, want one to repo/image:1", inner.pushCalls)
	}
	if r := reporter.Reports(); len(r) != 1 || r[0].Err != nil {
		t.Errorf("reports = %+v, want one success", r)
	}
}

func TestOciPushOutputHandler_LoadFiresPush(t *testing.T) {
	inner := &fakeInnerHandler{imageID: "sha256:abc", pushRes: pushResult{skipped: true}}
	reporter := NewPushReporter(nil)
	h := NewOciPushOutputHandler(inner, reporter, func() bool { return true })

	cached := &gen.Output{
		Kind: &gen.Output_OciImage{OciImage: &gen.OCIImageOutput{
			LocalTag: "repo/image:1", ImageId: "sha256:abc", PushDestination: "repo/image:1",
		}},
	}
	if err := h.Load(context.Background(), newTestTarget(t, "tgt"), cached, newTestProgress(t)); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if inner.loadCalls.Load() != 1 {
		t.Errorf("inner Load called %d times, want 1", inner.loadCalls.Load())
	}
	if len(inner.pushCalls) != 1 {
		t.Fatalf("push called %d times, want 1", len(inner.pushCalls))
	}
	if r := reporter.Reports(); len(r) != 1 || !r[0].Skipped {
		t.Errorf("expected one skipped report, got %+v", r)
	}
}

func TestOciPushOutputHandler_LoadPushFailureDoesNotPropagate(t *testing.T) {
	pushErr := errors.New("registry down")
	inner := &fakeInnerHandler{imageID: "sha256:abc", pushRes: pushResult{err: pushErr}}
	reporter := NewPushReporter(nil)
	h := NewOciPushOutputHandler(inner, reporter, func() bool { return true })

	cached := &gen.Output{
		Kind: &gen.Output_OciImage{OciImage: &gen.OCIImageOutput{
			LocalTag: "x", ImageId: "sha256:abc", PushDestination: "repo/x:1",
		}},
	}
	if err := h.Load(context.Background(), newTestTarget(t, "tgt"), cached, newTestProgress(t)); err != nil {
		t.Fatalf("Load returned %v; push failures must not propagate from Load", err)
	}
	if !reporter.HasFailures() {
		t.Error("expected reporter to record failure")
	}
}

func TestOciPushOutputHandler_FailFastAbortsSubsequent(t *testing.T) {
	pushErr := errors.New("first failure")
	inner := &fakeInnerHandler{imageID: "sha256:abc", innerPlan: NewNopWritePlan(), pushRes: pushResult{err: pushErr}}
	reporter := NewPushReporter(func() bool { return true })
	h := NewOciPushOutputHandler(inner, reporter, func() bool { return true })

	prepared1, _ := h.Write(context.Background(), newTestTarget(t, "tgt"), model.NewOutput(string(OciPushHandler), "a"), newTestProgress(t))
	prepared2, _ := h.Write(context.Background(), newTestTarget(t, "tgt"), model.NewOutput(string(OciPushHandler), "b"), newTestProgress(t))

	if err := prepared1.WritePlan.Execute(context.Background(), newTestProgress(t)); err == nil {
		t.Fatal("expected first push to fail")
	}
	pushesBefore := len(inner.pushCalls)
	if err := prepared2.WritePlan.Execute(context.Background(), newTestProgress(t)); err == nil {
		t.Fatal("expected second push to short-circuit")
	}
	if len(inner.pushCalls) != pushesBefore {
		t.Errorf("inner.PushImage was invoked after abort (was %d, now %d)", pushesBefore, len(inner.pushCalls))
	}
}
