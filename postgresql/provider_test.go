package postgresql

import (
	"context"
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
)

var testAccProviders map[string]*schema.Provider
var testAccProvider *schema.Provider

func init() {
	testAccProvider = Provider()
	testAccProviders = map[string]*schema.Provider{
		"postgresql": testAccProvider,
	}
}

func TestProvider(t *testing.T) {
	if err := Provider().InternalValidate(); err != nil {
		t.Fatalf("err: %s", err)
	}
}

func TestProvider_impl(t *testing.T) {
	var _ = Provider()
}

func TestProviderConfigureMaxIdleConnections(t *testing.T) {
	raw := map[string]interface{}{
		"host":                 "localhost",
		"port":                 5432,
		"username":             "postgres",
		"max_idle_connections": 7,
	}

	d := schema.TestResourceDataRaw(t, Provider().Schema, raw)

	meta, err := providerConfigure(d)
	if err != nil {
		t.Fatalf("providerConfigure returned an error: %s", err)
	}

	client, ok := meta.(*Client)
	if !ok {
		t.Fatalf("expected *Client, got %T", meta)
	}

	if client.config.MaxIdleConns != 7 {
		t.Errorf("expected MaxIdleConns to be 7, got %d", client.config.MaxIdleConns)
	}
}

func testAccPreCheck(t *testing.T) {
	var host string
	if host = os.Getenv("PGHOST"); host == "" {
		t.Fatal("PGHOST must be set for acceptance tests")
	}
	if v := os.Getenv("PGUSER"); v == "" {
		t.Fatal("PGUSER must be set for acceptance tests")
	}

	err := testAccProvider.Configure(context.Background(), terraform.NewResourceConfigRaw(nil))
	if err != nil {
		t.Fatal(err)
	}
}
