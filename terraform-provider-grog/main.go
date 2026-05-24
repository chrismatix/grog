// terraform-provider-grog exposes grog builds and image pushes as Terraform
// resources, so infrastructure-as-code can order a fast, cached grog build (and
// the resulting image digest) within Terraform's own dependency graph.
package main

//go:generate go run github.com/hashicorp/terraform-plugin-docs/cmd/tfplugindocs generate --provider-name grog

import (
	"context"
	"flag"
	"log"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"

	"github.com/chrismatix/grog/terraform-provider-grog/internal/provider"
)

// version is set at build time via -ldflags by the release pipeline.
var version = "dev"

func main() {
	var debug bool
	flag.BoolVar(&debug, "debug", false, "set to true to run the provider with support for debuggers like delve")
	flag.Parse()

	err := providerserver.Serve(context.Background(), provider.New(version), providerserver.ServeOpts{
		// The address under which the provider is published on the Terraform
		// Registry. Update the namespace before the first registry release.
		Address: "registry.terraform.io/chrismatix/grog",
		Debug:   debug,
	})
	if err != nil {
		log.Fatal(err.Error())
	}
}
