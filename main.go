package main

import (
	"context"
	"log"

	"github.com/hashicorp/terraform-plugin-sdk/plugin"
	"github.com/hashicorp/terraform-plugin-sdk/terraform"

	"github.com/terraform-providers/terraform-provider-postgresql/postgresql"
)

func main() {
	ctx := context.Background()
	plugin.Serve(&plugin.ServeOpts{
		ProviderFunc: func() terraform.ResourceProvider { return postgresql.Provider(ctx) },
	})
	log.Println("[DEBUG] Stopping plugin")
	// Wait for any context cleanup to happen
	<-ctx.Done()
	postgresql.WaitForRunningCommands()
}
