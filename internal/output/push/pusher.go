// Package push turns a target's oci_push map into the work that ships its
// images. Keeping it out of the output registry means a push is triggered by
// the completion path a target took, not by the fact that its outputs were
// touched — loading a dependency's outputs is not a completion.
package push

import (
	"context"
	"fmt"
	"maps"
	"slices"

	"grog/internal/model"
	"grog/internal/output/handlers"
	"grog/internal/proto/gen"
	"grog/internal/worker"
)

// Pusher owns everything --push: which images ship where, and the summary the
// build prints once it is done.
type Pusher struct {
	images   handlers.ImagePusher
	reporter *handlers.PushReporter
	enabled  func() bool
}

// New wires a Pusher to the oci output handler. An oci handler that cannot
// push is a wiring mistake in grog itself, not a user error.
func New(ociHandler handlers.Handler, enabled, failFast func() bool) (*Pusher, error) {
	images, ok := ociHandler.(handlers.ImagePusher)
	if !ok {
		return nil, fmt.Errorf("oci handler %T does not implement handlers.ImagePusher", ociHandler)
	}
	if enabled == nil {
		enabled = func() bool { return false }
	}
	return &Pusher{
		images:   images,
		reporter: handlers.NewPushReporter(failFast),
		enabled:  enabled,
	}, nil
}

// Reporter aggregates every push outcome for the end-of-build summary.
func (p *Pusher) Reporter() *handlers.PushReporter {
	if p == nil {
		return nil
	}
	return p.reporter
}

// Enabled reports whether --push is on. A nil Pusher never pushes, so callers
// that do not wire one keep working.
func (p *Pusher) Enabled() bool {
	return p != nil && p.enabled()
}

// PlansFromCache returns one plan per (oci output, destination), each sourcing
// the image grog staged in the cache. A key matching no oci:: output is a
// recipe bug, so it fails the target rather than landing in the push summary.
func (p *Pusher) PlansFromCache(target *model.Target, outputs []*gen.Output) ([]handlers.OutputWritePlan, error) {
	if !p.Enabled() {
		return nil, nil
	}
	var plans []handlers.OutputWritePlan
	for _, destination := range destinationsOf(target) {
		image := findOciImageByLocalTag(outputs, destination.localName)
		if image == nil {
			return nil, fmt.Errorf("%s: oci_push key %q does not match any oci:: output", target.Label, destination.localName)
		}
		plans = append(plans, handlers.NewOciPushPlan(
			p.images, image, destination.reference, target.Label.String(), p.reporter,
		))
	}
	return plans, nil
}

// PlansFromDaemon sources straight from the local docker daemon: targets that
// skip the cache never stage their oci outputs, so the plans from
// PlansFromCache would have nothing to read.
func (p *Pusher) PlansFromDaemon(target *model.Target) []handlers.OutputWritePlan {
	if !p.Enabled() {
		return nil
	}
	var plans []handlers.OutputWritePlan
	for _, destination := range destinationsOf(target) {
		plans = append(plans, handlers.NewLocalOciPushPlan(
			p.images, destination.localName, destination.reference, target.Label.String(), p.reporter,
		))
	}
	return plans
}

// PushFromCache ships a cache-hit target's destinations inline. The image comes
// from the cache, so this runs whether or not the outputs were loaded. A
// registry hiccup lands in the reporter rather than invalidating the cache
// restore, so only a malformed oci_push map returns an error.
func (p *Pusher) PushFromCache(
	ctx context.Context,
	target *model.Target,
	targetResult *gen.TargetResult,
	progress *worker.ProgressTracker,
) error {
	plans, err := p.PlansFromCache(target, targetResult.GetOutputs())
	if err != nil {
		return err
	}
	for _, plan := range plans {
		_ = plan.Execute(ctx, progress)
	}
	return nil
}

type destination struct {
	localName string
	reference string
}

// destinationsOf flattens target.OciPush in a stable order, so neither the
// order pushes run in nor the summary that reports them depends on map
// iteration order.
func destinationsOf(target *model.Target) []destination {
	var destinations []destination
	for _, localName := range slices.Sorted(maps.Keys(target.OciPush)) {
		for _, reference := range target.OciPush[localName] {
			destinations = append(destinations, destination{localName: localName, reference: reference})
		}
	}
	return destinations
}

func findOciImageByLocalTag(outputs []*gen.Output, localTag string) *gen.OCIImageOutput {
	for _, output := range outputs {
		image := output.GetOciImage()
		if image == nil {
			continue
		}
		if image.GetLocalTag() == localTag {
			return image
		}
	}
	return nil
}
