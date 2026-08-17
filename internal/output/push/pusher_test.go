package push

import (
	"context"
	"testing"

	"grog/internal/label"
	"grog/internal/model"
	"grog/internal/output/handlers"
	"grog/internal/proto/gen"
	"grog/internal/worker"
)

type fakeOciHandler struct {
	pushed      []string
	localPushed []string
}

func (f *fakeOciHandler) Type() handlers.HandlerType { return handlers.OCIHandler }

func (f *fakeOciHandler) Write(context.Context, model.Target, model.Output, *worker.ProgressTracker) (*handlers.PreparedOutput, error) {
	return nil, nil
}

func (f *fakeOciHandler) Hash(context.Context, model.Target, model.Output) (string, error) {
	return "", nil
}

func (f *fakeOciHandler) Load(context.Context, model.Target, *gen.Output, *worker.ProgressTracker) error {
	return nil
}

func (f *fakeOciHandler) PushImage(_ context.Context, _ *gen.OCIImageOutput, destination string, _ *worker.ProgressTracker) (bool, error) {
	f.pushed = append(f.pushed, destination)
	return false, nil
}

func (f *fakeOciHandler) PushLocalImage(_ context.Context, _ string, destination string, _ *worker.ProgressTracker) (bool, error) {
	f.localPushed = append(f.localPushed, destination)
	return false, nil
}

// nonPushingHandler is a Handler that lacks the ImagePusher capability.
type nonPushingHandler struct{ handlers.Handler }

func TestNewRejectsHandlerThatCannotPush(t *testing.T) {
	if _, err := New(nonPushingHandler{}, func() bool { return true }, nil); err == nil {
		t.Fatal("expected an error for an oci handler that does not implement ImagePusher")
	}
}

func newTestPusher(t *testing.T, enabled bool) (*Pusher, *fakeOciHandler) {
	t.Helper()
	handler := &fakeOciHandler{}
	pusher, err := New(handler, func() bool { return enabled }, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return pusher, handler
}

func testTarget(ociPush map[string][]string) *model.Target {
	return &model.Target{Label: label.TL("pkg", "tgt"), OciPush: ociPush}
}

func cachedOutputs(localTags ...string) []*gen.Output {
	outputs := make([]*gen.Output, 0, len(localTags))
	for _, localTag := range localTags {
		outputs = append(outputs, &gen.Output{
			Kind: &gen.Output_OciImage{OciImage: &gen.OCIImageOutput{LocalTag: localTag}},
		})
	}
	return outputs
}

func TestPlansFromCacheOnePerDestination(t *testing.T) {
	pusher, _ := newTestPusher(t, true)
	target := testTarget(map[string][]string{"app": {"registry.test/app:1", "registry.test/app:latest"}})

	plans, err := pusher.PlansFromCache(target, cachedOutputs("app"))
	if err != nil {
		t.Fatalf("PlansFromCache: %v", err)
	}
	if len(plans) != 2 {
		t.Fatalf("got %d plans, want one per destination", len(plans))
	}
}

func TestPlansFromCacheRejectsUnknownLocalName(t *testing.T) {
	pusher, _ := newTestPusher(t, true)
	target := testTarget(map[string][]string{"typo": {"registry.test/app:1"}})

	if _, err := pusher.PlansFromCache(target, cachedOutputs("app")); err == nil {
		t.Fatal("an oci_push key matching no oci:: output must fail the target")
	}
}

func TestPlansAreEmptyWithoutPushFlag(t *testing.T) {
	pusher, _ := newTestPusher(t, false)
	target := testTarget(map[string][]string{"app": {"registry.test/app:1"}})

	plans, err := pusher.PlansFromCache(target, cachedOutputs("app"))
	if err != nil {
		t.Fatalf("PlansFromCache: %v", err)
	}
	if len(plans) != 0 || len(pusher.PlansFromDaemon(target)) != 0 {
		t.Error("--push is off, so no target should produce push plans")
	}
}

// A no-cache target has nothing staged, so its plans must read the daemon.
func TestPlansFromDaemonBypassTheCache(t *testing.T) {
	pusher, handler := newTestPusher(t, true)
	target := testTarget(map[string][]string{"app": {"registry.test/app:1"}})

	for _, plan := range pusher.PlansFromDaemon(target) {
		if err := plan.Execute(context.Background(), newProgress()); err != nil {
			t.Fatalf("Execute: %v", err)
		}
	}

	if len(handler.pushed) != 0 {
		t.Errorf("cache-sourced push used for a no-cache target: %v", handler.pushed)
	}
	if len(handler.localPushed) != 1 {
		t.Errorf("daemon pushes = %v, want one", handler.localPushed)
	}
}

func TestPushFromCacheShipsEveryDestinationInOrder(t *testing.T) {
	pusher, handler := newTestPusher(t, true)
	target := testTarget(map[string][]string{
		"beta":  {"registry.test/beta:1"},
		"alpha": {"registry.test/alpha:1"},
	})
	targetResult := &gen.TargetResult{Outputs: cachedOutputs("alpha", "beta")}

	if err := pusher.PushFromCache(context.Background(), target, targetResult, newProgress()); err != nil {
		t.Fatalf("PushFromCache: %v", err)
	}

	want := []string{"registry.test/alpha:1", "registry.test/beta:1"}
	if len(handler.pushed) != len(want) {
		t.Fatalf("pushed %v, want %v", handler.pushed, want)
	}
	for index := range want {
		if handler.pushed[index] != want[index] {
			t.Fatalf("pushed %v, want %v (map iteration order must not leak)", handler.pushed, want)
		}
	}
}

func TestNilPusherNeverPushes(t *testing.T) {
	var pusher *Pusher
	target := testTarget(map[string][]string{"app": {"registry.test/app:1"}})

	plans, err := pusher.PlansFromCache(target, cachedOutputs("app"))
	if err != nil || len(plans) != 0 {
		t.Errorf("PlansFromCache on a nil Pusher = (%v, %v), want no plans and no error", plans, err)
	}
	if pusher.Enabled() || pusher.Reporter() != nil {
		t.Error("a nil Pusher must report itself as disabled")
	}
}

func newProgress() *worker.ProgressTracker {
	return worker.NewProgressTracker("test", 0, func(worker.StatusUpdate) {})
}
