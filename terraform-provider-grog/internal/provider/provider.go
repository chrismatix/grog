package provider

import (
	"context"
	"os"
	"path/filepath"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"grog/session"
)

// Ensure grogProvider satisfies the provider.Provider interface.
var _ provider.Provider = (*grogProvider)(nil)

// grogProvider is the Terraform provider for grog.
type grogProvider struct {
	// version is set at build time and surfaced to Terraform.
	version string
}

// grogProviderModel maps the provider configuration block.
type grogProviderModel struct {
	WorkspaceRoot     types.String `tfsdk:"workspace_root"`
	Profile           types.String `tfsdk:"profile"`
	SkipWorkspaceLock types.Bool   `tfsdk:"skip_workspace_lock"`
}

// New returns a provider factory for the given build version.
func New(version string) func() provider.Provider {
	return func() provider.Provider {
		return &grogProvider{version: version}
	}
}

func (p *grogProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "grog"
	resp.Version = p.version
}

func (p *grogProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Drive cached, incremental grog builds and image pushes from Terraform. " +
			"The provider loads a single grog workspace and serves all grog_build/grog_image_push resources from it.",
		Attributes: map[string]schema.Attribute{
			"workspace_root": schema.StringAttribute{
				Optional: true,
				MarkdownDescription: "Absolute or module-relative path to the grog workspace (the directory containing `grog.toml`). " +
					"If omitted, the provider walks up from the current working directory to find `grog.toml`. " +
					"Set this explicitly under Terragrunt, where the working directory is usually not the repo root.",
			},
			"profile": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Optional grog configuration profile to load (`grog.<profile>.toml`).",
			},
			"skip_workspace_lock": schema.BoolAttribute{
				Optional: true,
				MarkdownDescription: "Disable grog's cross-process workspace lock. Only set this if you can guarantee no other " +
					"grog process touches the workspace during the Terraform run; otherwise the cache may be corrupted.",
			},
		},
	}
}

func (p *grogProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var cfg grogProviderModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}

	workspaceRoot := cfg.WorkspaceRoot.ValueString()
	if workspaceRoot == "" {
		discovered, err := findWorkspaceRoot()
		if err != nil {
			resp.Diagnostics.AddError(
				"Could not locate grog workspace",
				"workspace_root was not set and no grog.toml was found in the current directory or any parent. "+
					"Set the provider's workspace_root attribute. Underlying error: "+err.Error(),
			)
			return
		}
		workspaceRoot = discovered
	}

	sess, err := session.New(ctx, session.Options{
		WorkspaceRoot:     workspaceRoot,
		Profile:           cfg.Profile.ValueString(),
		SkipWorkspaceLock: cfg.SkipWorkspaceLock.ValueBool(),
		LogWriter:         newTflogWriter(ctx),
	})
	if err != nil {
		resp.Diagnostics.AddError("Failed to initialize grog session", err.Error())
		return
	}

	// Resources receive the shared session via their Configure method.
	resp.ResourceData = sess
}

func (p *grogProvider) Resources(_ context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		NewBuildResource,
		NewImagePushResource,
	}
}

func (p *grogProvider) DataSources(_ context.Context) []func() datasource.DataSource {
	return nil
}

// findWorkspaceRoot walks up from the current working directory looking for
// grog.toml, mirroring the grog CLI's discovery.
func findWorkspaceRoot() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(cwd, "grog.toml")); err == nil {
			return cwd, nil
		}
		parent := filepath.Dir(cwd)
		if parent == cwd {
			return "", os.ErrNotExist
		}
		cwd = parent
	}
}
