package handlers

import (
	"encoding/json"
	"testing"

	"github.com/docker/docker/pkg/jsonmessage"
)

func TestFormatPhaseSummary_NoLayers(t *testing.T) {
	got := formatPhaseSummary(map[string]string{})
	if got != "" {
		t.Fatalf("expected empty summary when no layers, got %q", got)
	}
}

func TestFormatPhaseSummary_SingleLayer(t *testing.T) {
	got := formatPhaseSummary(map[string]string{
		"layer-1": "Pushing",
	})
	want := "1 pushing"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TestFormatPhaseSummary_OrderIsStable verifies that phases come out in the
// documented preference order regardless of map iteration order. The summary
// is recomputed on every layer transition, so flicker between two adjacent
// transitions would be confusing if it depended on map order.
func TestFormatPhaseSummary_OrderIsStable(t *testing.T) {
	states := map[string]string{
		"l1": "Pushed",
		"l2": "Preparing",
		"l3": "Pushing",
		"l4": "Pushing",
		"l5": "Waiting",
	}
	got := formatPhaseSummary(states)
	want := "2 pushing, 1 preparing, 1 waiting, 1 pushed"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TestFormatPhaseSummary_UnknownPhase ensures phases the docker daemon emits
// that aren't in our preferred-order list still get rendered (sorted
// alphabetically and after the known ones) rather than being silently dropped.
func TestFormatPhaseSummary_UnknownPhase(t *testing.T) {
	got := formatPhaseSummary(map[string]string{
		"l1": "Pushing",
		"l2": "Surprise!",
	})
	want := "1 pushing, 1 surprise!"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TestFormatPhaseSummary_PluralizationOfLayerAlreadyExists is a regression
// test: previously the daemon's "Layer already exists" status was lowercased
// and prefixed with the count, producing the ungrammatical "2 layer already
// exists". The fix is a noun-shaped short label ("cached") that reads
// correctly with any count.
func TestFormatPhaseSummary_PluralizationOfLayerAlreadyExists(t *testing.T) {
	got := formatPhaseSummary(map[string]string{
		"l1": "Layer already exists",
		"l2": "Layer already exists",
		"l3": "Layer already exists",
	})
	want := "3 cached"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TestFormatPhaseSummary_PullStateLabels verifies the pull-side daemon
// statuses get sensible noun-shaped labels too — "1 downloaded" rather than
// "1 download complete".
func TestFormatPhaseSummary_PullStateLabels(t *testing.T) {
	got := formatPhaseSummary(map[string]string{
		"l1": "Downloading",
		"l2": "Download complete",
		"l3": "Extracting",
		"l4": "Pull complete",
	})
	want := "1 downloading, 1 extracting, 1 downloaded, 1 pulled"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// The two image stores report a completed push differently: the classic store
// sends a structured aux payload, the containerd store only the status line.
func TestRegistryConfirmedDigest(t *testing.T) {
	const digest = "sha256:fa647fc1e5d5df7d8d923fb6332aab8e78783f8fca1a1394efb4011f68f5a793"

	aux := json.RawMessage(`{"Tag":"1.0.0","Digest":"` + digest + `","Size":1917}`)
	cases := []struct {
		name    string
		message jsonmessage.JSONMessage
		want    string
	}{
		{"classic aux", jsonmessage.JSONMessage{Aux: &aux}, digest},
		{"containerd status line", jsonmessage.JSONMessage{Status: "1.0.0: digest: " + digest + " size: 1917"}, digest},
		{"layer progress", jsonmessage.JSONMessage{ID: "abc", Status: "Pushing"}, ""},
		{"unrelated aux", jsonmessage.JSONMessage{Aux: rawMessage(`{"manifestPushedInsteadOfIndex":true}`)}, ""},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := registryConfirmedDigest(c.message); got != c.want {
				t.Errorf("registryConfirmedDigest = %q, want %q", got, c.want)
			}
		})
	}
}

func rawMessage(s string) *json.RawMessage {
	raw := json.RawMessage(s)
	return &raw
}
