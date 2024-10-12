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
	var _ *schema.Provider = Provider()
}

func TestAccProviderSetCreateRoleSelfGrant(t *testing.T) {
	skipIfNotAcc(t)

	config := getTestConfig(t)
	client := config.NewClient("postgres")
	db, err := client.Connect()
	if err != nil {
		t.Fatal(err)
	}

	if db.featureSupported(featureCreateRoleSelfGrant) {
		t.Skipf("Skip tests for unsuported feature %d in Postgres %s", featureCreateRoleSelfGrant, db.version)
	}

	// Create NON superuser role
	if _, err = db.Exec("CREATE ROLE rds_srg LOGIN CREATEDB CREATEROLE PASSWORD 'rds_srg'"); err != nil {
		t.Fatalf("could not create role for test user paramaters: %v", err)
	}
	defer func() {
		_, _ = db.Exec("DROP ROLE rds_srg")
	}()

	provider := Provider()
	provider.Configure(context.Background(), terraform.NewResourceConfigRaw(
		map[string]interface{}{
			"username": "rds_srg",
			"password": "rds_srg",
		},
	))
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
