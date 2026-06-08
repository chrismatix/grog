package handlers

import (
	"strings"
	"testing"

	"grog/internal/worker"
)

func TestConsumeDockerProgress_NilReader(t *testing.T) {
	if err := consumeDockerProgress(nil, &worker.ProgressTracker{}, "status"); err != nil {
		t.Fatalf("nil reader: %v", err)
	}
}

func TestConsumeDockerProgress_ParsesMessages(t *testing.T) {
	stream := strings.Join([]string{
		`{"id":"l1","status":"Preparing"}`,
		`{"id":"l1","status":"Pushing","progressDetail":{"current":10,"total":100}}`,
		`{"id":"l1","status":"Pushing","progressDetail":{"current":100,"total":100}}`,
		`{"id":"l2","status":"Layer already exists"}`,
		`{"status":"latest: digest: sha256:abc size: 1234"}`,
	}, "\n")
	tracker := worker.NewProgressTracker("status", 0, nil)
	if err := consumeDockerProgress(strings.NewReader(stream), tracker, "status"); err != nil {
		t.Fatalf("consume: %v", err)
	}
}

func TestConsumeDockerProgress_BadJSON(t *testing.T) {
	if err := consumeDockerProgress(strings.NewReader("{not json"), worker.NewProgressTracker("status", 0, nil), "status"); err == nil {
		t.Fatal("expected err")
	}
}

func TestConsumeDockerProgress_ErrorMessage(t *testing.T) {
	stream := `{"errorDetail":{"code":1,"message":"boom"}}`
	if err := consumeDockerProgress(strings.NewReader(stream), worker.NewProgressTracker("status", 0, nil), "status"); err == nil {
		t.Fatal("expected daemon err propagation")
	}
}
