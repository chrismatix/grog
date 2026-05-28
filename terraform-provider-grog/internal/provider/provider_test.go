package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	fwprovider "github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
)

// TestProviderSchema runs the framework's full schema validation over the
// provider and all its resources via the protocol server. This catches schema
// wiring mistakes (invalid attribute combinations, bad nested types, etc.)
// without needing a terraform binary.
func TestProviderSchema(t *testing.T) {
	ctx := context.Background()
	server := providerserver.NewProtocol6(New("test")())()

	resp, err := server.GetProviderSchema(ctx, &tfprotov6.GetProviderSchemaRequest{})
	if err != nil {
		t.Fatalf("GetProviderSchema: %v", err)
	}
	for _, d := range resp.Diagnostics {
		if d.Severity == tfprotov6.DiagnosticSeverityError {
			t.Errorf("schema diagnostic: %s — %s", d.Summary, d.Detail)
		}
	}

	wantResources := []string{"grog_build", "grog_image_push"}
	for _, name := range wantResources {
		if _, ok := resp.ResourceSchemas[name]; !ok {
			t.Errorf("missing resource schema %q", name)
		}
	}
}

// TestResourceMetadata asserts the resource type names so renames are caught.
func TestResourceMetadata(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		newFn func() resource.Resource
		want  string
	}{
		{NewBuildResource, "grog_build"},
		{NewImagePushResource, "grog_image_push"},
	}
	for _, tc := range cases {
		var resp resource.MetadataResponse
		tc.newFn().Metadata(ctx, resource.MetadataRequest{ProviderTypeName: "grog"}, &resp)
		if resp.TypeName != tc.want {
			t.Errorf("TypeName = %q, want %q", resp.TypeName, tc.want)
		}
	}
}

// compile-time assertions that the provider and resources implement the
// expected interfaces.
var (
	_ fwprovider.Provider            = (*grogProvider)(nil)
	_ resource.Resource              = (*buildResource)(nil)
	_ resource.Resource              = (*imagePushResource)(nil)
	_ resource.ResourceWithConfigure = (*buildResource)(nil)
	_ resource.ResourceWithConfigure = (*imagePushResource)(nil)
	_ datasource.DataSource          = nil
)
