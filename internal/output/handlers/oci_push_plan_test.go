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

// fakeImagePusher records every PushImage call and returns the canned outcome
// the test set. It satisfies the ImagePusher interface without bringing up a
// docker daemon or a real registry.
type fakeImagePusher struct {
	calls   []pushCall
	skipped bool
	err     error
}

type pushCall struct {
	destination string
	imageID     string
}

func (f *fakeImagePusher) PushImage(_ context.Context, image *gen.OCIImageOutput, dest string, _ *worker.ProgressTracker) (bool, error) {
	f.calls = append(f.calls, pushCall{destination: dest, imageID: image.GetImageId()})
	return f.skipped, f.err
}

func newTestProgress(t *testing.T) *worker.ProgressTracker {
	t.Helper()
	return worker.NewProgressTracker("test", 0, func(worker.StatusUpdate) {})
}

func newTestPlan(pusher ImagePusher, reporter *PushReporter, dest string) *OciPushPlan {
	return NewOciPushPlan(pusher, &gen.OCIImageOutput{ImageId: "sha256:abc"}, dest, "//pkg:tgt", reporter)
}

func _newTarget(_ *testing.T) model.Target {
	return model.Target{Label: label.TL("pkg", "tgt")}
}

func TestOciPushPlan_Pushed(t *testing.T) {
	pusher := &fakeImagePusher{}
	reporter := NewPushReporter(nil)

	if err := newTestPlan(pusher, reporter, "repo/image:1").Execute(context.Background(), newTestProgress(t)); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(pusher.calls) != 1 {
		t.Fatalf("PushImage called %d times, want 1", len(pusher.calls))
	}
	if r := reporter.Reports(); len(r) != 1 || r[0].Skipped || r[0].Err != nil {
		t.Errorf("expected one pushed report, got %+v", r)
	}
}

func TestOciPushPlan_Skipped(t *testing.T) {
	pusher := &fakeImagePusher{skipped: true}
	reporter := NewPushReporter(nil)

	if err := newTestPlan(pusher, reporter, "repo/image:1").Execute(context.Background(), newTestProgress(t)); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if r := reporter.Reports(); len(r) != 1 || !r[0].Skipped {
		t.Errorf("expected one skipped report, got %+v", r)
	}
}

func TestOciPushPlan_Failed(t *testing.T) {
	pushErr := errors.New("503")
	pusher := &fakeImagePusher{err: pushErr}
	reporter := NewPushReporter(nil)

	if err := newTestPlan(pusher, reporter, "repo/image:1").Execute(context.Background(), newTestProgress(t)); !errors.Is(err, pushErr) {
		t.Errorf("Execute err = %v, want wrap of %v", err, pushErr)
	}
	if !reporter.HasFailures() {
		t.Error("expected reporter to record failure")
	}
}

func TestOciPushPlan_FailFastAborts(t *testing.T) {
	pushErr := errors.New("first failure")
	failing := &fakeImagePusher{err: pushErr}
	reporter := NewPushReporter(func() bool { return true })

	if err := newTestPlan(failing, reporter, "a").Execute(context.Background(), newTestProgress(t)); err == nil {
		t.Fatal("expected first push to fail")
	}
	subsequent := &fakeImagePusher{}
	if err := newTestPlan(subsequent, reporter, "b").Execute(context.Background(), newTestProgress(t)); err == nil {
		t.Fatal("expected second push to short-circuit")
	}
	if len(subsequent.calls) != 0 {
		t.Errorf("PushImage was invoked after abort (%d calls)", len(subsequent.calls))
	}
}
