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
	Target     types.String `tfsdk:"target"`
	ID          types.String `tfsdk:"id"`
	ChangeHash  types.String `tfsdk:"change_hash"`
	OutputHash  types.String `tfsdk:"output_hash"`
	CacheHit    types.Bool   `tfsdk:"cache_hit"`
	OCIImages   types.Map    `tfsdk:"oci_images"`
	Files       types.Map    `tfsdk:"files"`
	Directories types.Map    `tfsdk:"directories"`
}

// ociImageType is the object type for each entry in oci_images.
var ociImageType = types.ObjectType{
	AttrTypes: map[string]attr.Type{
		"identifier":      types.StringType,
		"image_id":        types.StringType,
		"manifest_digest": types.StringType,
	},
}

// fileOutputType is the object type for each entry in files.
var fileOutputType = types.ObjectType{
	AttrTypes: map[string]attr.Type{
		"path":          types.StringType,
		"digest":        types.StringType,
		"is_executable": types.BoolType,
	},
}

// directoryOutputType is the object type for each entry in directories.
var directoryOutputType = types.ObjectType{
	AttrTypes: map[string]attr.Type{
		"path":   types.StringType,
		"digest": types.StringType,
	},
}

func (r *buildResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_build"
}

func (r *buildResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Builds a grog target (and its dependency closure) and exposes its outputs. " +
			"The build runs on every apply; grog's content-addressed cache makes unchanged builds fast no-ops. " +
			"Image outputs are published only to grog's CAS — use `grog_image_push` to deliver them to a registry.\n\n" +
			"**Docker daemon side effect:** if the target builds an image via `docker build`, the resulting image is " +
			"also tagged in the local Docker daemon under the BUILD file's local tag (e.g. `my-image:latest`). That " +
			"lets you `docker run` the freshly built image locally for inspection without pulling. The push to a " +
			"registry is independent (and daemon-free) — see `grog_image_push`.",
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
				PlanModifiers: []planmodifier.Bool{
					knownAfterApplyBool(),
				},
			},
			"oci_images": schema.MapNestedAttribute{
				Computed: true,
				MarkdownDescription: "Container (OCI) image outputs keyed by their local tag (the image tag declared in the BUILD file). " +
					"Each value carries the `manifest_digest` to feed into `grog_image_push`. " +
					"Named `oci_images` rather than `docker_images` because the manifest format is OCI regardless of which tool produced it.",
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
			"files": schema.MapNestedAttribute{
				Computed: true,
				MarkdownDescription: "File outputs keyed by their package-relative path (the path declared in the BUILD file's `outputs`). " +
					"`path` is the workspace-absolute path on disk, re-derived from the current workspace root on each read.",
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"path": schema.StringAttribute{
							Computed:            true,
							MarkdownDescription: "Workspace-absolute path to the produced file.",
						},
						"digest": schema.StringAttribute{
							Computed:            true,
							MarkdownDescription: "Content hash of the file (algorithm per `grog.toml`).",
						},
						"is_executable": schema.BoolAttribute{
							Computed:            true,
							MarkdownDescription: "Whether grog marked the file executable (used for `bin_output` targets).",
						},
					},
				},
				PlanModifiers: []planmodifier.Map{
					knownAfterApplyMap(),
				},
			},
			"directories": schema.MapNestedAttribute{
				Computed: true,
				MarkdownDescription: "Directory outputs keyed by their package-relative path. " +
					"`digest` is grog's Merkle-tree hash of the directory contents.",
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"path": schema.StringAttribute{
							Computed:            true,
							MarkdownDescription: "Workspace-absolute path to the produced directory.",
						},
						"digest": schema.StringAttribute{
							Computed:            true,
							MarkdownDescription: "Merkle-tree digest of the directory contents.",
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

	images := make(map[string]attr.Value, len(result.OCIImages))
	for id, img := range result.OCIImages {
		obj, objDiags := types.ObjectValue(ociImageType.AttrTypes, map[string]attr.Value{
			"identifier":      types.StringValue(img.Identifier),
			"image_id":        types.StringValue(img.ImageID),
			"manifest_digest": types.StringValue(img.ManifestDigest),
		})
		diags.Append(objDiags...)
		images[id] = obj
	}
	imagesMap, imagesDiags := types.MapValue(ociImageType, images)
	diags.Append(imagesDiags...)
	model.OCIImages = imagesMap

	files := make(map[string]attr.Value, len(result.Files))
	for id, f := range result.Files {
		obj, objDiags := types.ObjectValue(fileOutputType.AttrTypes, map[string]attr.Value{
			"path":          types.StringValue(f.Path),
			"digest":        types.StringValue(f.Digest),
			"is_executable": types.BoolValue(f.IsExecutable),
		})
		diags.Append(objDiags...)
		files[id] = obj
	}
	filesMap, filesDiags := types.MapValue(fileOutputType, files)
	diags.Append(filesDiags...)
	model.Files = filesMap

	dirs := make(map[string]attr.Value, len(result.Directories))
	for id, d := range result.Directories {
		obj, objDiags := types.ObjectValue(directoryOutputType.AttrTypes, map[string]attr.Value{
			"path":   types.StringValue(d.Path),
			"digest": types.StringValue(d.Digest),
		})
		diags.Append(objDiags...)
		dirs[id] = obj
	}
	dirsMap, dirsDiags := types.MapValue(directoryOutputType, dirs)
	diags.Append(dirsDiags...)
	model.Directories = dirsMap
}
