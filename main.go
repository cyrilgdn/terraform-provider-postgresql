package main

import (
	"github.com/hashicorp/terraform/plugin"
	"github.com/terraform-providers/terraform-provider-postgresql/postgresql"
)

func main() {
	plugin.Serve(&plugin.ServeOpts{
		ProviderFunc: postgresql.Provider})
}
