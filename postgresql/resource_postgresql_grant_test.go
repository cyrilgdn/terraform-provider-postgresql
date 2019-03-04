package postgresql

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform/helper/resource"
	"github.com/hashicorp/terraform/terraform"
)

func TestAccPostgresqlGrant(t *testing.T) {
	// We have to create the database outside of resource.Test
	// because we need to create a table to assert that grant are correctly applied
	// and we don't have this resource yet
	dbSuffix, teardown := setupTestDatabase(t, true, true, true)
	defer teardown()

	dbName, roleName := getTestDBNames(dbSuffix)
	var testGrantSelect = fmt.Sprintf(`
	resource "postgresql_grant" "test_ro" {
		database    = "%s"
		role        = "%s"
		schema      = "public"
		object_type = "table"
		privileges   = ["SELECT"]
	}
	`, dbName, roleName)

	var testGrantSelectInsertUpdate = fmt.Sprintf(`
	resource "postgresql_grant" "test_ro" {
		database    = "%s"
		role        = "%s"
		schema      = "public"
		object_type = "table"
		privileges   = ["SELECT", "INSERT", "UPDATE"]
	}
	`, dbName, roleName)

	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccPreCheck(t)
			testCheckCompatibleVersion(t, featurePrivileges)
		},
		Providers: testAccProviders,
		Steps: []resource.TestStep{
			{
				Config: testGrantSelect,
				Check: resource.ComposeTestCheckFunc(
					func(*terraform.State) error {
						return testCheckTablePrivileges(t, dbSuffix, []string{"SELECT"}, false)
					},
					resource.TestCheckResourceAttr("postgresql_grant.test_ro", "privileges.#", "1"),
					resource.TestCheckResourceAttr("postgresql_grant.test_ro", "privileges.3138006342", "SELECT"),
				),
			},
			{
				Config: testGrantSelectInsertUpdate,
				Check: resource.ComposeTestCheckFunc(
					func(*terraform.State) error {
						return testCheckTablePrivileges(t, dbSuffix, []string{"SELECT", "INSERT", "UPDATE"}, false)
					},
					resource.TestCheckResourceAttr("postgresql_grant.test_ro", "privileges.#", "3"),
					resource.TestCheckResourceAttr("postgresql_grant.test_ro", "privileges.3138006342", "SELECT"),
					resource.TestCheckResourceAttr("postgresql_grant.test_ro", "privileges.892623219", "INSERT"),
					resource.TestCheckResourceAttr("postgresql_grant.test_ro", "privileges.1759376126", "UPDATE"),
				),
			},
		},
	})
}
