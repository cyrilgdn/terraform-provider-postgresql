package postgresql

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
)

// Basic create: a single declared grant is applied and observable.
func TestAccPostgresqlDefaultPrivilegesPolicy_Basic(t *testing.T) {
	skipIfNotAcc(t)

	// We have to create the database outside of resource.Test
	// because we need to create a table to assert that grants are correctly applied
	// and we don't have this resource yet.
	dbSuffix, teardown := setupTestDatabase(t, true, true)
	defer teardown()

	config := getTestConfig(t)
	dbName, roleName := getTestDBNames(dbSuffix)

	// We set PGUSER as owner as it will create the test table.
	tfConfig := fmt.Sprintf(`
resource "postgresql_default_privileges_policy" "test" {
  database = "%[1]s"
  owner    = "%[2]s"

  grants = [
    {
      schema            = "test_schema"
      role              = "%[3]s"
      object_type       = "table"
      privileges        = ["SELECT"]
      with_grant_option = false
    }
  ]
}
`, dbName, config.Username, roleName)

	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccPreCheck(t)
			testCheckCompatibleVersion(t, featurePrivileges)
		},
		Providers: testAccProviders,
		Steps: []resource.TestStep{
			{
				Config: tfConfig,
				Check: resource.ComposeTestCheckFunc(
					func(*terraform.State) error {
						tables := []string{"test_schema.test_table"}
						// To test default privileges, we need to create a table
						// after having applied the state.
						dropFunc := createTestTables(t, dbSuffix, tables, "")
						defer dropFunc()

						return testCheckTablesPrivileges(t, dbName, roleName, tables, []string{"SELECT"})
					},
					resource.TestCheckResourceAttr("postgresql_default_privileges_policy.test", "authoritative", "false"),
					resource.TestCheckResourceAttr("postgresql_default_privileges_policy.test", "grants.#", "1"),
					resource.TestCheckResourceAttr("postgresql_default_privileges_policy.test", "grants.0.schema", "test_schema"),
					resource.TestCheckResourceAttr("postgresql_default_privileges_policy.test", "grants.0.object_type", "table"),
					resource.TestCheckResourceAttr("postgresql_default_privileges_policy.test", "grants.0.privileges.#", "1"),
					resource.TestCheckResourceAttr("postgresql_default_privileges_policy.test", "id", fmt.Sprintf("%s_%s", dbName, config.Username)),
				),
			},
		},
	})
}

// Update: adding a grant, widening privileges on an existing one, then
// removing a previously declared grant.
func TestAccPostgresqlDefaultPrivilegesPolicy_Update(t *testing.T) {
	skipIfNotAcc(t)

	dbSuffix, teardown := setupTestDatabase(t, true, true)
	defer teardown()

	config := getTestConfig(t)
	dbName, roleName := getTestDBNames(dbSuffix)

	step1Config := fmt.Sprintf(`
resource "postgresql_default_privileges_policy" "test" {
  database = "%[1]s"
  owner    = "%[2]s"

  grants = [
    {
      schema      = "test_schema"
      role        = "%[3]s"
      object_type = "table"
      privileges  = ["SELECT"]
      with_grant_option = false
    }
  ]
}
`, dbName, config.Username, roleName)

	// Adds a grant on dev_schema and widens privileges on the test_schema grant.
	step2Config := fmt.Sprintf(`
resource "postgresql_default_privileges_policy" "test" {
  database = "%[1]s"
  owner    = "%[2]s"

  grants = [
    {
      schema      = "test_schema"
      role        = "%[3]s"
      object_type = "table"
      privileges  = ["SELECT", "UPDATE"]
      with_grant_option = false
    },
    {
      schema      = "dev_schema"
      role        = "%[3]s"
      object_type = "table"
      privileges  = ["SELECT"]
      with_grant_option = false
    }
  ]
}
`, dbName, config.Username, roleName)

	// Removes the test_schema grant: in additive (non-authoritative) mode this
	// just stops managing it going forward, only dev_schema stays declared.
	step3Config := fmt.Sprintf(`
resource "postgresql_default_privileges_policy" "test" {
  database = "%[1]s"
  owner    = "%[2]s"

  grants = [
    {
      schema      = "dev_schema"
      role        = "%[3]s"
      object_type = "table"
      privileges  = ["SELECT"]
      with_grant_option = false
    }
  ]
}
`, dbName, config.Username, roleName)

	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccPreCheck(t)
			testCheckCompatibleVersion(t, featurePrivileges)
		},
		Providers: testAccProviders,
		Steps: []resource.TestStep{
			{
				Config: step1Config,
				Check: resource.ComposeTestCheckFunc(
					func(*terraform.State) error {
						tables := []string{"test_schema.test_table"}
						dropFunc := createTestTables(t, dbSuffix, tables, "")
						defer dropFunc()

						return testCheckTablesPrivileges(t, dbName, roleName, tables, []string{"SELECT"})
					},
					resource.TestCheckResourceAttr("postgresql_default_privileges_policy.test", "grants.#", "1"),
				),
			},
			{
				Config: step2Config,
				Check: resource.ComposeTestCheckFunc(
					func(*terraform.State) error {
						tables := []string{"test_schema.test_table"}
						dropFunc := createTestTables(t, dbSuffix, tables, "")
						defer dropFunc()

						return testCheckTablesPrivileges(t, dbName, roleName, tables, []string{"SELECT", "UPDATE"})
					},
					func(*terraform.State) error {
						tables := []string{"dev_schema.test_table"}
						dropFunc := createTestTables(t, dbSuffix, tables, "")
						defer dropFunc()

						return testCheckTablesPrivileges(t, dbName, roleName, tables, []string{"SELECT"})
					},
					resource.TestCheckResourceAttr("postgresql_default_privileges_policy.test", "grants.#", "2"),
				),
			},
			{
				Config: step3Config,
				Check: resource.ComposeTestCheckFunc(
					func(*terraform.State) error {
						tables := []string{"dev_schema.test_table"}
						dropFunc := createTestTables(t, dbSuffix, tables, "")
						defer dropFunc()

						return testCheckTablesPrivileges(t, dbName, roleName, tables, []string{"SELECT"})
					},
					resource.TestCheckResourceAttr("postgresql_default_privileges_policy.test", "grants.#", "1"),
				),
			},
		},
	})
}

// Authoritative behavior: with authoritative = false, a default privilege
// grant created outside of this resource must survive a re-apply. Flipping
// to authoritative = true must revoke it on the next apply.
func TestAccPostgresqlDefaultPrivilegesPolicy_Authoritative(t *testing.T) {
	skipIfNotAcc(t)

	dbSuffix, teardown := setupTestDatabase(t, true, true)
	defer teardown()

	config := getTestConfig(t)
	dbName, roleName := getTestDBNames(dbSuffix)

	unmanagedRoleName := fmt.Sprintf("%s_unmanaged", roleName)
	dropUnmanagedRole := createTestRole(t, unmanagedRoleName)
	defer dropUnmanagedRole()

	// The unmanaged role needs USAGE on the schema to even attempt queries.
	// This grant must be revoked before dropUnmanagedRole runs (it fires
	// before setupTestDatabase's teardown, since defers unwind LIFO), or
	// DROP ROLE fails with "cannot be dropped because some objects depend on it".
	dbExecute(t, config.connStr(dbName), fmt.Sprintf(
		"GRANT usage ON SCHEMA test_schema TO %s", unmanagedRoleName,
	))
	defer dbExecute(t, config.connStr(dbName), fmt.Sprintf(
		"REVOKE usage ON SCHEMA test_schema FROM %s", unmanagedRoleName,
	))

	nonAuthoritativeConfig := fmt.Sprintf(`
resource "postgresql_default_privileges_policy" "test" {
  database      = "%[1]s"
  owner         = "%[2]s"
  authoritative = false

  grants = [
    {
      schema      = "test_schema"
      role        = "%[3]s"
      object_type = "table"
      privileges  = ["SELECT"]
      with_grant_option = false
    }
  ]
}
`, dbName, config.Username, roleName)

	// Same as above plus a harmless second grant, purely to force Terraform to
	// detect a diff and re-run Create/Update (an unchanged config would produce
	// an empty plan and never re-execute the reconciliation logic at all).
	nonAuthoritativeConfigWithSecondGrant := fmt.Sprintf(`
resource "postgresql_default_privileges_policy" "test" {
  database      = "%[1]s"
  owner         = "%[2]s"
  authoritative = false

  grants = [
    {
      schema      = "test_schema"
      role        = "%[3]s"
      object_type = "table"
      privileges  = ["SELECT"]
      with_grant_option = false
    },
    {
      schema      = "dev_schema"
      role        = "%[3]s"
      object_type = "table"
      privileges  = ["SELECT"]
      with_grant_option = false
    }
  ]
}
`, dbName, config.Username, roleName)

	authoritativeConfig := fmt.Sprintf(`
resource "postgresql_default_privileges_policy" "test" {
  database      = "%[1]s"
  owner         = "%[2]s"
  authoritative = true

  grants = [
    {
      schema      = "test_schema"
      role        = "%[3]s"
      object_type = "table"
      privileges  = ["SELECT"]
      with_grant_option = false
    },
    {
      schema      = "dev_schema"
      role        = "%[3]s"
      object_type = "table"
      privileges  = ["SELECT"]
      with_grant_option = false
    }
  ]
}
`, dbName, config.Username, roleName)

	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccPreCheck(t)
			testCheckCompatibleVersion(t, featurePrivileges)
		},
		Providers: testAccProviders,
		Steps: []resource.TestStep{
			{
				// authoritative = false: apply only the declared grant.
				Config: nonAuthoritativeConfig,
				Check: resource.ComposeTestCheckFunc(
					func(*terraform.State) error {
						tables := []string{"test_schema.test_table"}
						dropFunc := createTestTables(t, dbSuffix, tables, "")
						defer dropFunc()

						return testCheckTablesPrivileges(t, dbName, roleName, tables, []string{"SELECT"})
					},
					// Manually inject a default privilege grant this resource never declared.
					func(*terraform.State) error {
						dbExecute(t, config.connStr(dbName), fmt.Sprintf(
							"ALTER DEFAULT PRIVILEGES FOR ROLE %s IN SCHEMA test_schema GRANT SELECT ON TABLES TO %s",
							config.Username, unmanagedRoleName,
						))
						return nil
					},
				),
			},
			{
				// Re-apply with a real (additive) change: the unmanaged grant must
				// survive since authoritative = false never revokes what it did
				// not declare.
				Config: nonAuthoritativeConfigWithSecondGrant,
				Check: resource.ComposeTestCheckFunc(
					func(*terraform.State) error {
						tables := []string{"test_schema.test_table"}
						dropFunc := createTestTables(t, dbSuffix, tables, "")
						defer dropFunc()

						return testCheckTablesPrivileges(t, dbName, unmanagedRoleName, tables, []string{"SELECT"})
					},
					resource.TestCheckResourceAttr("postgresql_default_privileges_policy.test", "grants.#", "2"),
				),
			},
			{
				// Flip to authoritative = true: the unmanaged grant is still not
				// declared in `grants`, so it must be revoked on this apply.
				Config: authoritativeConfig,
				Check: resource.ComposeTestCheckFunc(
					func(*terraform.State) error {
						tables := []string{"test_schema.test_table"}
						dropFunc := createTestTables(t, dbSuffix, tables, "")
						defer dropFunc()

						return testCheckTablesPrivileges(t, dbName, unmanagedRoleName, tables, []string{})
					},
					func(*terraform.State) error {
						tables := []string{"test_schema.test_table"}
						dropFunc := createTestTables(t, dbSuffix, tables, "")
						defer dropFunc()

						return testCheckTablesPrivileges(t, dbName, roleName, tables, []string{"SELECT"})
					},
				),
			},
		},
	})
}

// Import: an existing set of default privileges, created outside of
// Terraform, can be imported and matches what a subsequent apply would
// produce.
func TestAccPostgresqlDefaultPrivilegesPolicy_Import(t *testing.T) {
	skipIfNotAcc(t)

	dbSuffix, teardown := setupTestDatabase(t, true, true)
	defer teardown()

	config := getTestConfig(t)
	dbName, roleName := getTestDBNames(dbSuffix)

	tfConfig := fmt.Sprintf(`
resource "postgresql_default_privileges_policy" "test" {
  database      = "%[1]s"
  owner         = "%[2]s"
  authoritative = false

  grants = [
    {
      schema      = "test_schema"
      role        = "%[3]s"
      object_type = "table"
      privileges  = ["SELECT"]
      with_grant_option = false
    }
  ]
}
`, dbName, config.Username, roleName)

	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccPreCheck(t)
			testCheckCompatibleVersion(t, featurePrivileges)
		},
		Providers: testAccProviders,
		Steps: []resource.TestStep{
			{
				Config: tfConfig,
			},
			{
				ResourceName: "postgresql_default_privileges_policy.test",
				ImportState:  true,
				ImportStateIdFunc: func(*terraform.State) (string, error) {
					return fmt.Sprintf("%s/%s", dbName, config.Username), nil
				},
				ImportStateVerify: true,
				// Import never writes managed_grants by design — it legitimately
				// differs (empty) from the ledger a real apply populates.
				ImportStateVerifyIgnore: []string{"managed_grants"},
			},
		},
	})
}
