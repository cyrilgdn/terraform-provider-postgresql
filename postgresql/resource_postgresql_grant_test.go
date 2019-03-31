package postgresql

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform/helper/resource"
	"github.com/hashicorp/terraform/terraform"
)

func TestAccPostgresqlGrant(t *testing.T) {
	// We have to create the database outside of resource.Test
	// because we need to create tables to assert that grant are correctly applied
	// and we don't have this resource yet
	dbSuffix, teardown := setupTestDatabase(t, true, true)
	defer teardown()

	testTables := []string{"test_table", "test_table2"}
	createTestTables(t, dbSuffix, testTables)

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
					resource.TestCheckResourceAttr("postgresql_grant.test_ro", "privileges.#", "1"),
					resource.TestCheckResourceAttr("postgresql_grant.test_ro", "privileges.3138006342", "SELECT"),
					func(*terraform.State) error {
						return testCheckTablesPrivileges(t, dbSuffix, testTables, []string{"SELECT"})
					},
				),
			},
			{
				Config: testGrantSelectInsertUpdate,
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("postgresql_grant.test_ro", "privileges.#", "3"),
					resource.TestCheckResourceAttr("postgresql_grant.test_ro", "privileges.3138006342", "SELECT"),
					resource.TestCheckResourceAttr("postgresql_grant.test_ro", "privileges.892623219", "INSERT"),
					resource.TestCheckResourceAttr("postgresql_grant.test_ro", "privileges.1759376126", "UPDATE"),
					func(*terraform.State) error {
						return testCheckTablesPrivileges(t, dbSuffix, testTables, []string{"SELECT", "INSERT", "UPDATE"})
					},
				),
			},
		},
	})
}
