package postgresql

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/terraform"
)

func TestAccPostgresqlDefaultPrivileges(t *testing.T) {
	skipIfNotAcc(t)

	// We have to create the database outside of resource.Test
	// because we need to create a table to assert that grant are correctly applied
	// and we don't have this resource yet
	dbSuffix, teardown := setupTestDatabase(t, true, true)
	defer teardown()

	config := getTestConfig(t)
	dbName, roleName := getTestDBNames(dbSuffix)

	// Set default privileges to the test role then to public (i.e.: everyone)
	for _, role := range []string{roleName, "public"} {
		t.Run(role, func(t *testing.T) {
			withGrant := true
			if role == "public" {
				withGrant = false
			}

			// We set PGUSER as owner as he will create the test table
			var tfConfig = fmt.Sprintf(`
resource "postgresql_default_privileges" "test_ro" {
	database    = "%s"
	owner       = "%s"
	role        = "%s"
	schema      = "test_schema"
	object_type = "table"
	with_grant_option = %t
	privileges   = %%s
}
	`, dbName, config.Username, role, withGrant)

			resource.Test(t, resource.TestCase{
				PreCheck: func() {
					testAccPreCheck(t)
					testCheckCompatibleVersion(t, featurePrivileges)
				},
				Providers: testAccProviders,
				Steps: []resource.TestStep{
					{
						Config: fmt.Sprintf(tfConfig, `[]`),
						Check: resource.ComposeTestCheckFunc(
							func(*terraform.State) error {
								tables := []string{"test_schema.test_table"}
								// To test default privileges, we need to create a table
								// after having apply the state.
								dropFunc := createTestTables(t, dbSuffix, tables, "")
								defer dropFunc()

								return testCheckTablesPrivileges(t, dbName, roleName, tables, []string{})
							},
							resource.TestCheckResourceAttr("postgresql_default_privileges.test_ro", "object_type", "table"),
							resource.TestCheckResourceAttr("postgresql_default_privileges.test_ro", "with_grant_option", fmt.Sprintf("%t", withGrant)),
							resource.TestCheckResourceAttr("postgresql_default_privileges.test_ro", "privileges.#", "0"),
						),
					},
					{
						Config: fmt.Sprintf(tfConfig, `["SELECT"]`),
						Check: resource.ComposeTestCheckFunc(
							func(*terraform.State) error {
								tables := []string{"test_schema.test_table"}
								// To test default privileges, we need to create a table
								// after having apply the state.
								dropFunc := createTestTables(t, dbSuffix, tables, "")
								defer dropFunc()

								return testCheckTablesPrivileges(t, dbName, roleName, tables, []string{"SELECT"})
							},
							resource.TestCheckResourceAttr("postgresql_default_privileges.test_ro", "object_type", "table"),
							resource.TestCheckResourceAttr("postgresql_default_privileges.test_ro", "with_grant_option", fmt.Sprintf("%t", withGrant)),
							resource.TestCheckResourceAttr("postgresql_default_privileges.test_ro", "privileges.#", "1"),
							resource.TestCheckResourceAttr("postgresql_default_privileges.test_ro", "privileges.3138006342", "SELECT"),
						),
					},
					{
						Config: fmt.Sprintf(tfConfig, `["SELECT", "UPDATE"]`),
						Check: resource.ComposeTestCheckFunc(
							func(*terraform.State) error {
								tables := []string{"test_schema.test_table"}
								// To test default privileges, we need to create a table
								// after having apply the state.
								dropFunc := createTestTables(t, dbSuffix, tables, "")
								defer dropFunc()

								return testCheckTablesPrivileges(t, dbName, roleName, tables, []string{"SELECT", "UPDATE"})
							},
							resource.TestCheckResourceAttr("postgresql_default_privileges.test_ro", "privileges.#", "2"),
							resource.TestCheckResourceAttr("postgresql_default_privileges.test_ro", "privileges.3138006342", "SELECT"),
							resource.TestCheckResourceAttr("postgresql_default_privileges.test_ro", "privileges.1759376126", "UPDATE"),
						),
					},
					{
						Config: fmt.Sprintf(tfConfig, `[]`),
						Check: resource.ComposeTestCheckFunc(
							func(*terraform.State) error {
								tables := []string{"test_schema.test_table"}
								// To test default privileges, we need to create a table
								// after having apply the state.
								dropFunc := createTestTables(t, dbSuffix, tables, "")
								defer dropFunc()

								return testCheckTablesPrivileges(t, dbName, roleName, tables, []string{})
							},
							resource.TestCheckResourceAttr("postgresql_default_privileges.test_ro", "object_type", "table"),
							resource.TestCheckResourceAttr("postgresql_default_privileges.test_ro", "with_grant_option", fmt.Sprintf("%t", withGrant)),
							resource.TestCheckResourceAttr("postgresql_default_privileges.test_ro", "privileges.#", "0"),
						),
					},
				},
			})
		})
	}
}

// Test the case where we need to grant the owner to the connected user.
// The owner should be revoked
func TestAccPostgresqlDefaultPrivileges_GrantOwner(t *testing.T) {
	skipIfNotAcc(t)

	// We have to create the database outside of resource.Test
	// because we need to create a table to assert that grant are correctly applied
	// and we don't have this resource yet
	dbSuffix, teardown := setupTestDatabase(t, true, true)
	defer teardown()

	config := getTestConfig(t)
	dsn := config.connStr("postgres")
	dbName, roleName := getTestDBNames(dbSuffix)

	// We set PGUSER as owner as he will create the test table
	var stateConfig = fmt.Sprintf(`

resource postgresql_role "test_owner" {
       name = "test_owner"
}

resource "postgresql_default_privileges" "test_ro" {
	database    = "%s"
	owner       = postgresql_role.test_owner.name
	role        = "%s"
	schema      = "public"
	object_type = "table"
	privileges   = ["SELECT"]
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
				Config: stateConfig,
				Check: resource.ComposeTestCheckFunc(
					func(*terraform.State) error {
						tables := []string{"public.test_table"}
						// To test default privileges, we need to create a table
						// after having apply the state.
						dropFunc := createTestTables(t, dbSuffix, tables, "test_owner")
						defer dropFunc()

						return testCheckTablesPrivileges(t, dbName, roleName, tables, []string{"SELECT"})
					},
					resource.TestCheckResourceAttr("postgresql_default_privileges.test_ro", "object_type", "table"),
					resource.TestCheckResourceAttr("postgresql_default_privileges.test_ro", "privileges.#", "1"),
					resource.TestCheckResourceAttr("postgresql_default_privileges.test_ro", "privileges.3138006342", "SELECT"),

					// check if connected user does not have test_owner granted anymore.
					checkUserMembership(t, dsn, config.Username, "test_owner", false),
				),
			},
		},
	})
}

// Test the case where we define default priviliges without specifying a schema. These
// priviliges should apply to newly created resources for the named role in all schema.
func TestAccPostgresqlDefaultPrivileges_NoSchema(t *testing.T) {
	skipIfNotAcc(t)

	// We have to create the database outside of resource.Test
	// because we need to create a table to assert that grant are correctly applied
	// and we don't have this resource yet
	dbSuffix, teardown := setupTestDatabase(t, true, true)
	defer teardown()

	config := getTestConfig(t)
	dbName, roleName := getTestDBNames(dbSuffix)

	// Set default privileges to the test role then to public (i.e.: everyone)
	for _, role := range []string{roleName, "public"} {
		t.Run(role, func(t *testing.T) {

			hclText := `
resource "postgresql_default_privileges" "test_ro" {
	database    = "%s"
	owner       = "%s"
	role        = "%s"
	object_type = "table"
	privileges   = %%s
}
`
			// We set PGUSER as owner as he will create the test table
			var tfConfig = fmt.Sprintf(hclText, dbName, config.Username, role)

			resource.Test(t, resource.TestCase{
				PreCheck: func() {
					testAccPreCheck(t)
					testCheckCompatibleVersion(t, featurePrivileges)
				},
				Providers: testAccProviders,
				Steps: []resource.TestStep{
					{
						Config: fmt.Sprintf(tfConfig, `["SELECT"]`),
						Check: resource.ComposeTestCheckFunc(
							func(*terraform.State) error {
								tables := []string{"test_schema.test_table", "dev_schema.test_table"}
								// To test default privileges, we need to create tables
								// in both dev and test schema after having applied the state.
								dropFunc := createTestTables(t, dbSuffix, tables, "")
								defer dropFunc()

								return testCheckTablesPrivileges(t, dbName, roleName, tables, []string{"SELECT"})
							},
							resource.TestCheckResourceAttr("postgresql_default_privileges.test_ro", "object_type", "table"),
							resource.TestCheckResourceAttr("postgresql_default_privileges.test_ro", "privileges.#", "1"),
							resource.TestCheckResourceAttr("postgresql_default_privileges.test_ro", "privileges.3138006342", "SELECT"),
						),
					},
					{
						Config: fmt.Sprintf(tfConfig, `["SELECT", "UPDATE"]`),
						Check: resource.ComposeTestCheckFunc(
							func(*terraform.State) error {
								tables := []string{"test_schema.test_table", "dev_schema.test_table"}
								// To test default privileges, we need to create tables
								// in both dev and test schema after having applied the state.
								dropFunc := createTestTables(t, dbSuffix, tables, "")
								defer dropFunc()

								return testCheckTablesPrivileges(t, dbName, roleName, tables, []string{"SELECT", "UPDATE"})
							},
							resource.TestCheckResourceAttr("postgresql_default_privileges.test_ro", "privileges.#", "2"),
							resource.TestCheckResourceAttr("postgresql_default_privileges.test_ro", "privileges.3138006342", "SELECT"),
							resource.TestCheckResourceAttr("postgresql_default_privileges.test_ro", "privileges.1759376126", "UPDATE"),
						),
					},
				},
			})
		})
	}
}
