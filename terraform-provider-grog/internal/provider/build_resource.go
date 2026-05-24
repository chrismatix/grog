package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"grog/session"
)

var (
	_ resource.Resource              = (*buildResource)(nil)
	_ resource.ResourceWithConfigure = (*buildResource)(nil)
)

// buildResource implements grog_build.
type buildResource struct {
	session *session.Session
}

// NewBuildResource is the resource factory registered with the provider.
func NewBuildResource() resource.Resource {
	return &buildResource{}
}

// buildResourceModel is the Terraform state model for grog_build.
type buildResourceModel struct {
	Target       types.String `tfsdk:"target"`
	ID           types.String `tfsdk:"id"`
	ChangeHash   types.String `tfsdk:"change_hash"`
	OutputHash   types.String `tfsdk:"output_hash"`
	CacheHit     types.Bool   `tfsdk:"cache_hit"`
	DockerImages types.Map    `tfsdk:"docker_images"`
}

// dockerImageType is the object type for each entry in docker_images.
var dockerImageType = types.ObjectType{
	AttrTypes: map[string]attr.Type{
		"identifier":      types.StringType,
		"image_id":        types.StringType,
		"manifest_digest": types.StringType,
	},
}

func (r *buildResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_build"
}

func (r *buildResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Builds a grog target (and its dependency closure) and exposes its outputs. " +
			"The build runs on every apply; grog's content-addressed cache makes unchanged builds fast no-ops. " +
			"Docker outputs are published only to grog's CAS — use `grog_image_push` to deliver them to a registry.",
		Attributes: map[string]schema.Attribute{
			"target": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "The grog target label to build, e.g. `//services/api:image`.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Resource identifier (the target label).",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"change_hash": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "grog's content hash of the target definition, inputs, and dependency outputs (the cache key).",
				PlanModifiers: []planmodifier.String{
					knownAfterApply(),
				},
			},
			"output_hash": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "grog's hash of the produced outputs.",
				PlanModifiers: []planmodifier.String{
					knownAfterApply(),
				},
			},
			"cache_hit": schema.BoolAttribute{
				Computed:            true,
				MarkdownDescription: "Whether the most recent build was served from cache rather than executed.",
			},
			"docker_images": schema.MapNestedAttribute{
				Computed: true,
				MarkdownDescription: "Docker image outputs keyed by their local tag (the image tag declared in the BUILD file). " +
					"Each value carries the `manifest_digest` to feed into `grog_image_push`.",
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"identifier": schema.StringAttribute{
							Computed:            true,
							MarkdownDescription: "The output identifier / local image tag from the BUILD file.",
						},
						"image_id": schema.StringAttribute{
							Computed:            true,
							MarkdownDescription: "The image config sha256 (docker image ID).",
						},
						"manifest_digest": schema.StringAttribute{
							Computed:            true,
							MarkdownDescription: "The OCI manifest digest (`sha256:…`), the content-addressed handle for pushing.",
						},
					},
				},
				PlanModifiers: []planmodifier.Map{
					knownAfterApplyMap(),
				},
			},
		},
	}
}

func (r *buildResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *buildResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan buildResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	r.build(ctx, &plan, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *buildResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan buildResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	r.build(ctx, &plan, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// Read is a no-op: there is no external footprint to refresh. The build is
// re-run on every apply (the computed outputs are marked known-after-apply),
// so drift is reconciled at apply time rather than refresh time.
func (r *buildResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state buildResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Delete is a no-op: a built artifact cannot be "un-built", and grog's CAS entry
// is content-addressed and shared, so it is left intact. The resource is simply
// dropped from state.
func (r *buildResource) Delete(_ context.Context, _ resource.DeleteRequest, _ *resource.DeleteResponse) {
}

// build runs the grog build and populates the computed fields on the model.
func (r *buildResource) build(ctx context.Context, model *buildResourceModel, diags diagSink) {
	if r.session == nil {
		diags.AddError("Provider not configured", "the grog session is not available")
		return
	}

	target := model.Target.ValueString()
	result, err := r.session.Build(ctx, target)
	if err != nil {
		diags.AddError("grog build failed", err.Error())
		return
	}

	model.ID = types.StringValue(result.Label)
	model.ChangeHash = types.StringValue(result.ChangeHash)
	model.OutputHash = types.StringValue(result.OutputHash)
	model.CacheHit = types.BoolValue(result.CacheHit)

	images := make(map[string]attr.Value, len(result.DockerImages))
	for id, img := range result.DockerImages {
		obj, objDiags := types.ObjectValue(dockerImageType.AttrTypes, map[string]attr.Value{
			"identifier":      types.StringValue(img.Identifier),
			"image_id":        types.StringValue(img.ImageID),
			"manifest_digest": types.StringValue(img.ManifestDigest),
		})
		diags.Append(objDiags...)
		images[id] = obj
	}
	mapVal, mapDiags := types.MapValue(dockerImageType, images)
	diags.Append(mapDiags...)
	model.DockerImages = mapVal
}
