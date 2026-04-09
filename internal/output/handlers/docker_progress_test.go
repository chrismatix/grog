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
