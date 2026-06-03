package handlers

import (
	"context"
	"errors"
	"testing"

	"grog/internal/label"
	"grog/internal/model"
	"grog/internal/proto/gen"
	"grog/internal/worker"
)

// fakeImagePusher stands in for a docker handler in tests that exercise the
// ociPushPlan / PushReporter contract without bringing up a real registry.
type fakeImagePusher struct {
	calls []pushCall
	res   pushResult
}

type pushCall struct {
	destination string
	imageID     string
}

type pushResult struct {
	skipped bool
	err     error
}

func (f *fakeImagePusher) PushImage(_ context.Context, image *gen.DockerImageOutput, destination string, _ *worker.ProgressTracker) (bool, error) {
	f.calls = append(f.calls, pushCall{destination: destination, imageID: image.GetImageId()})
	return f.res.skipped, f.res.err
}

func newTestTarget(t *testing.T, name string) model.Target {
	t.Helper()
	return model.Target{Label: label.TL("pkg", name)}
}

func newTestProgress(t *testing.T) *worker.ProgressTracker {
	t.Helper()
	return worker.NewProgressTracker("test", 0, func(worker.StatusUpdate) {})
}

func newTestPlan(pusher ImagePusher, reporter *PushReporter, destination string) *ociPushPlan {
	return &ociPushPlan{
		pusher:      pusher,
		dockerOut:   &gen.DockerImageOutput{ImageId: "sha256:abc"},
		destination: destination,
		targetLabel: "//pkg:tgt",
		reporter:    reporter,
	}
}

func TestOciPushPlan_PushedRecorded(t *testing.T) {
	pusher := &fakeImagePusher{}
	reporter := NewPushReporter(nil)
	plan := newTestPlan(pusher, reporter, "repo/image:1")

	if err := plan.Execute(context.Background(), newTestProgress(t)); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(pusher.calls) != 1 {
		t.Fatalf("pusher called %d times, want 1", len(pusher.calls))
	}
	reports := reporter.Reports()
	if len(reports) != 1 || reports[0].Skipped || reports[0].Err != nil {
		t.Errorf("expected one pushed report, got %+v", reports)
	}
}

func TestOciPushPlan_SkippedRecorded(t *testing.T) {
	pusher := &fakeImagePusher{res: pushResult{skipped: true}}
	reporter := NewPushReporter(nil)
	plan := newTestPlan(pusher, reporter, "repo/image:1")

	if err := plan.Execute(context.Background(), newTestProgress(t)); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if r := reporter.Reports(); len(r) != 1 || !r[0].Skipped {
		t.Errorf("expected one skipped report, got %+v", r)
	}
}

func TestOciPushPlan_FailureRecordedAndReturned(t *testing.T) {
	pushErr := errors.New("503")
	pusher := &fakeImagePusher{res: pushResult{err: pushErr}}
	reporter := NewPushReporter(nil)
	plan := newTestPlan(pusher, reporter, "repo/image:1")

	if err := plan.Execute(context.Background(), newTestProgress(t)); !errors.Is(err, pushErr) {
		t.Errorf("Execute err = %v, want wrap of %v", err, pushErr)
	}
	if !reporter.HasFailures() {
		t.Error("expected reporter to record failure")
	}
}

func TestOciPushPlan_FailFastAbortsSubsequent(t *testing.T) {
	pushErr := errors.New("first failure")
	failingPusher := &fakeImagePusher{res: pushResult{err: pushErr}}
	reporter := NewPushReporter(func() bool { return true })

	if err := newTestPlan(failingPusher, reporter, "a").Execute(context.Background(), newTestProgress(t)); err == nil {
		t.Fatal("expected first push to fail")
	}

	// Second plan must not hit the network at all once the reporter is aborted.
	nextPusher := &fakeImagePusher{}
	if err := newTestPlan(nextPusher, reporter, "b").Execute(context.Background(), newTestProgress(t)); err == nil {
		t.Fatal("expected second push to short-circuit")
	}
	if len(nextPusher.calls) != 0 {
		t.Errorf("subsequent pusher invoked after abort: %d calls", len(nextPusher.calls))
	}
}

func TestDockerOutputHandler_AttachPushPlan_OciPushType(t *testing.T) {
	// Verify the fs docker handler chains the push plan only when --push is
	// enabled, and that it always stamps push_destination so the cache
	// round-trips oci-push semantics.
	target := newTestTarget(t, "tgt")
	output := model.NewOutput(string(OciPushHandler), "repo/image:1")
	prepared := &PreparedOutput{
		Output:    &gen.Output{Kind: &gen.Output_DockerImage{DockerImage: &gen.DockerImageOutput{LocalTag: "x", ImageId: "sha256:abc"}}},
		WritePlan: NewNopWritePlan(),
	}
	image := prepared.Output.GetDockerImage()

	h := &DockerOutputHandler{pushEnabled: func() bool { return false }, pushReporter: NewPushReporter(nil)}
	h.maybeAttachPushPlan(prepared, image, output, target)

	if image.GetPushDestination() != "repo/image:1" {
		t.Errorf("push_destination = %q, want %q", image.GetPushDestination(), "repo/image:1")
	}
	if _, isComposite := prepared.WritePlan.(*CompositeWritePlan); isComposite {
		t.Errorf("expected raw plan when --push is off, got composite")
	}

	h.pushEnabled = func() bool { return true }
	prepared.WritePlan = NewNopWritePlan()
	h.maybeAttachPushPlan(prepared, image, output, target)
	if _, isComposite := prepared.WritePlan.(*CompositeWritePlan); !isComposite {
		t.Errorf("expected composite plan when --push is on, got %T", prepared.WritePlan)
	}
}
