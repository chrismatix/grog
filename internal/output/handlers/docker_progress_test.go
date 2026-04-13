package handlers

import "testing"

func TestFormatLayerPhaseSummary_NoLayers(t *testing.T) {
	got := formatLayerPhaseSummary("base", map[string]string{})
	if got != "base" {
		t.Fatalf("expected unchanged base when no layers, got %q", got)
	}
}

func TestFormatLayerPhaseSummary_SingleLayer(t *testing.T) {
	got := formatLayerPhaseSummary("//foo:bar: caching docker image foo", map[string]string{
		"layer-1": "Pushing",
	})
	want := "//foo:bar: caching docker image foo (1 pushing)"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TestFormatLayerPhaseSummary_OrderIsStable verifies that phases come out in
// the documented preference order regardless of map iteration order. The
// summary is recomputed on every layer transition, so flicker between two
// adjacent transitions would be confusing if it depended on map order.
func TestFormatLayerPhaseSummary_OrderIsStable(t *testing.T) {
	states := map[string]string{
		"l1": "Pushed",
		"l2": "Preparing",
		"l3": "Pushing",
		"l4": "Pushing",
		"l5": "Waiting",
	}
	got := formatLayerPhaseSummary("base", states)
	want := "base (2 pushing, 1 preparing, 1 waiting, 1 pushed)"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TestFormatLayerPhaseSummary_UnknownPhase ensures phases the docker daemon
// emits that aren't in our preferred-order list still get rendered (sorted
// alphabetically and after the known ones) rather than being silently dropped.
func TestFormatLayerPhaseSummary_UnknownPhase(t *testing.T) {
	got := formatLayerPhaseSummary("base", map[string]string{
		"l1": "Pushing",
		"l2": "Surprise!",
	})
	want := "base (1 pushing, 1 surprise!)"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TestFormatLayerPhaseSummary_PluralizationOfLayerAlreadyExists is a
// regression test: previously the daemon's "Layer already exists" status was
// lowercased and prefixed with the count, producing the ungrammatical
// "2 layer already exists". The fix is a noun-shaped short label ("cached")
// that reads correctly with any count.
func TestFormatLayerPhaseSummary_PluralizationOfLayerAlreadyExists(t *testing.T) {
	got := formatLayerPhaseSummary("base", map[string]string{
		"l1": "Layer already exists",
		"l2": "Layer already exists",
		"l3": "Layer already exists",
	})
	want := "base (3 cached)"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TestFormatLayerPhaseSummary_PullStateLabels verifies the pull-side daemon
// statuses get sensible noun-shaped labels too — "1 downloaded" rather than
// "1 download complete".
func TestFormatLayerPhaseSummary_PullStateLabels(t *testing.T) {
	got := formatLayerPhaseSummary("base", map[string]string{
		"l1": "Downloading",
		"l2": "Download complete",
		"l3": "Extracting",
		"l4": "Pull complete",
	})
	want := "base (1 downloading, 1 extracting, 1 downloaded, 1 pulled)"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
