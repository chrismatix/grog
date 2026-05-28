package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"grog/session"
)

var (
	_ resource.Resource              = (*imagePushResource)(nil)
	_ resource.ResourceWithConfigure = (*imagePushResource)(nil)
)

// imagePushResource implements grog_image_push.
type imagePushResource struct {
	session *session.Session
}

// NewImagePushResource is the resource factory registered with the provider.
func NewImagePushResource() resource.Resource {
	return &imagePushResource{}
}

// imagePushResourceModel is the Terraform state model for grog_image_push.
type imagePushResourceModel struct {
	SourceDigest types.String `tfsdk:"source_digest"`
	Repository   types.String `tfsdk:"repository"`
	Tags         types.List   `tfsdk:"tags"`
	ID           types.String `tfsdk:"id"`
	Reference    types.String `tfsdk:"reference"`
	PushedTags   types.List   `tfsdk:"pushed_tags"`
}

func (r *imagePushResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_image_push"
}

func (r *imagePushResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Pushes a grog-built image (by manifest digest) from grog's content-addressed store to an " +
			"external registry, without the local Docker daemon. Authentication uses the ambient Docker keychain " +
			"(`~/.docker/config.json` and credential helpers, e.g. `gcloud auth configure-docker`). Convergent: a digest " +
			"already present at the destination is not re-pushed.",
		Attributes: map[string]schema.Attribute{
			"source_digest": schema.StringAttribute{
				Required: true,
				MarkdownDescription: "The OCI manifest digest of a built image (`sha256:…`), typically " +
					"`grog_build.<name>.oci_images[\"<tag>\"].manifest_digest`.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"repository": schema.StringAttribute{
				Required: true,
				MarkdownDescription: "Destination repository without a tag, e.g. " +
					"`us-docker.pkg.dev/my-project/my-repo/api`.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"tags": schema.ListAttribute{
				Optional:            true,
				ElementType:         types.StringType,
				MarkdownDescription: "Optional human-readable tags to also point at the digest (e.g. `[\"v1\", \"latest\"]`).",
			},
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Resource identifier (the pinned digest reference).",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"reference": schema.StringAttribute{
				Computed: true,
				MarkdownDescription: "The immutable pinned reference `<repository>@<digest>` to feed downstream resources " +
					"(e.g. a Cloud Run service's image).",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"pushed_tags": schema.ListAttribute{
				Computed:            true,
				ElementType:         types.StringType,
				MarkdownDescription: "The fully-qualified tag references that now point at the digest.",
			},
		},
	}
}

func (r *imagePushResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	sess, ok := req.ProviderData.(*session.Session)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected provider data",
			fmt.Sprintf("expected *session.Session, got %T", req.ProviderData),
		)
		return
	}
	r.session = sess
}

func (r *imagePushResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan imagePushResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	r.push(ctx, &plan, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *imagePushResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan imagePushResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	r.push(ctx, &plan, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// Read re-checks the destination registry and, if the digest is no longer
// present (deleted or garbage-collected), clears state so the next apply
// re-pushes. This gives true drift detection against the registry.
func (r *imagePushResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state imagePushResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if r.session == nil {
		return
	}

	present, err := r.session.ImageExists(ctx, state.Repository.ValueString(), state.SourceDigest.ValueString())
	if err != nil {
		// A transient registry error should not destroy state; surface a warning
		// and keep the current state.
		resp.Diagnostics.AddWarning("Could not verify pushed image", err.Error())
		return
	}
	if !present {
		// Drift: the digest is gone from the destination. Remove from state to
		// trigger a re-push on the next apply.
		resp.State.RemoveResource(ctx)
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Delete is a no-op: removing the Terraform resource does not delete the image
// from the registry (other consumers may depend on it). The resource is simply
// dropped from state.
func (r *imagePushResource) Delete(_ context.Context, _ resource.DeleteRequest, _ *resource.DeleteResponse) {
}

// push performs the image push and populates the computed fields.
func (r *imagePushResource) push(ctx context.Context, model *imagePushResourceModel, diags diagSink) {
	if r.session == nil {
		diags.AddError("Provider not configured", "the grog session is not available")
		return
	}

	var tags []string
	if !model.Tags.IsNull() && !model.Tags.IsUnknown() {
		diags.Append(model.Tags.ElementsAs(ctx, &tags, false)...)
		if diags.HasError() {
			return
		}
	}

	result, err := r.session.PushImage(ctx, session.PushOptions{
		ManifestDigest: model.SourceDigest.ValueString(),
		Repository:     model.Repository.ValueString(),
		Tags:           tags,
	})
	if err != nil {
		diags.AddError("grog image push failed", err.Error())
		return
	}

	model.ID = types.StringValue(result.Reference)
	model.Reference = types.StringValue(result.Reference)

	pushedTags, listDiags := types.ListValueFrom(ctx, types.StringType, result.Tags)
	diags.Append(listDiags...)
	model.PushedTags = pushedTags
}
