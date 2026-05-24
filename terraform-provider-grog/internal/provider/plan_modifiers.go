package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// diagSink is the subset of the diagnostics API the shared helpers need,
// satisfied by *diag.Diagnostics. It lets the build/push helpers append
// diagnostics without depending on a specific response type.
type diagSink = *diag.Diagnostics

// alwaysUnknownString marks a computed string attribute as known-after-apply on
// every plan where the resource is present. This implements the v1
// "always rebuild on apply" behavior: the real value (e.g. a digest) can only
// be computed by running the build, so the plan must show it — and any
// downstream consumers — as changing until apply resolves it.
type alwaysUnknownString struct{}

func knownAfterApply() planmodifier.String { return alwaysUnknownString{} }

func (alwaysUnknownString) Description(_ context.Context) string {
	return "Value is recomputed by grog on every apply."
}

func (alwaysUnknownString) MarkdownDescription(_ context.Context) string {
	return "Value is recomputed by grog on every apply."
}

func (alwaysUnknownString) PlanModifyString(_ context.Context, req planmodifier.StringRequest, resp *planmodifier.StringResponse) {
	// On destroy the whole plan is null; leave the value alone.
	if req.Plan.Raw.IsNull() {
		return
	}
	resp.PlanValue = types.StringUnknown()
}

// alwaysUnknownMap is the Map analogue of alwaysUnknownString.
type alwaysUnknownMap struct{}

func knownAfterApplyMap() planmodifier.Map { return alwaysUnknownMap{} }

func (alwaysUnknownMap) Description(_ context.Context) string {
	return "Value is recomputed by grog on every apply."
}

func (alwaysUnknownMap) MarkdownDescription(_ context.Context) string {
	return "Value is recomputed by grog on every apply."
}

func (alwaysUnknownMap) PlanModifyMap(ctx context.Context, req planmodifier.MapRequest, resp *planmodifier.MapResponse) {
	if req.Plan.Raw.IsNull() {
		return
	}
	resp.PlanValue = types.MapUnknown(req.PlanValue.ElementType(ctx))
}
