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

func TestProviderGCPOptions(t *testing.T) {
	p := Provider()
	for _, key := range []string{
		"gcp_ip_type",
		"gcp_iam_auth",
		"gcp_dns",
		"gcp_iam_impersonate_service_account",
	} {
		if _, ok := p.Schema[key]; !ok {
			t.Errorf("provider schema missing %q", key)
		}
	}

	// gcp_ip_type must accept "psc"
	v := p.Schema["gcp_ip_type"].ValidateFunc
	if v == nil {
		t.Fatal("gcp_ip_type has no ValidateFunc")
	}
	if _, errs := v("psc", "gcp_ip_type"); len(errs) != 0 {
		t.Errorf("gcp_ip_type rejected \"psc\": %v", errs)
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
