package postgresql

import (
	"context"
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/helper/schema"
	"github.com/hashicorp/terraform-plugin-sdk/terraform"
)

var testAccProviders map[string]terraform.ResourceProvider
var testAccProvider *schema.Provider

func init() {
	testAccProvider = Provider(context.TODO()).(*schema.Provider)
	testAccProviders = map[string]terraform.ResourceProvider{
		"postgresql": testAccProvider,
	}
}

func TestProvider(t *testing.T) {
	if err := Provider(context.TODO()).(*schema.Provider).InternalValidate(); err != nil {
		t.Fatalf("err: %s", err)
	}
}

func TestProvider_impl(t *testing.T) {
	var _ terraform.ResourceProvider = Provider(context.TODO())
}

func testAccPreCheck(t *testing.T) {
	var host string
	if host = os.Getenv("PGHOST"); host == "" {
		t.Fatal("PGHOST must be set for acceptance tests")
	}
	if v := os.Getenv("PGUSER"); v == "" {
		t.Fatal("PGUSER must be set for acceptance tests")
	}

	err := testAccProvider.Configure(terraform.NewResourceConfigRaw(nil))
	if err != nil {
		t.Fatal(err)
	}
}
