package postgresql

import (
	"database/sql"
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
)

// Basic create: a table-wide grant is applied and observable. Covers test
// matrix item 1 (create and clean read).
func TestAccPostgresqlGrantPolicy_Basic(t *testing.T) {
	skipIfNotAcc(t)

	dbSuffix, teardown := setupTestDatabase(t, true, true)
	defer teardown()

	dbName, roleName := getTestDBNames(dbSuffix)

	tables := []string{"test_schema.test_table"}
	dropTables := createTestTables(t, dbSuffix, tables, "")
	defer dropTables()

	tfConfig := fmt.Sprintf(`
resource "postgresql_grant_policy" "test" {
  database = "%[1]s"
  role     = "%[2]s"

  grants = [
    {
      schema      = "test_schema"
      object_type = "table"
      privileges  = ["SELECT"]
    }
  ]
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
				Config: tfConfig,
				Check: resource.ComposeTestCheckFunc(
					func(*terraform.State) error {
						return testCheckTablesPrivileges(t, dbName, roleName, tables, []string{"SELECT"})
					},
					resource.TestCheckResourceAttr("postgresql_grant_policy.test", "authoritative", "false"),
					resource.TestCheckResourceAttr("postgresql_grant_policy.test", "grants.#", "1"),
					resource.TestCheckResourceAttr("postgresql_grant_policy.test", "grants.0.schema", "test_schema"),
					resource.TestCheckResourceAttr("postgresql_grant_policy.test", "grants.0.object_type", "table"),
					resource.TestCheckResourceAttr("postgresql_grant_policy.test", "grants.0.privileges.#", "1"),
					resource.TestCheckResourceAttr("postgresql_grant_policy.test", "id", fmt.Sprintf("%s_%s", dbName, roleName)),
				),
			},
		},
	})
}

// Update: adding a grant, widening privileges on an existing one, then
// removing a previously declared grant. Covers test matrix items 2, 3, 12
// (add/remove privileges, add/remove entries, schema-level grants).
func TestAccPostgresqlGrantPolicy_Update(t *testing.T) {
	skipIfNotAcc(t)

	dbSuffix, teardown := setupTestDatabase(t, true, true)
	defer teardown()

	dbName, roleName := getTestDBNames(dbSuffix)

	tables := []string{"test_schema.test_table"}
	dropTables := createTestTables(t, dbSuffix, tables, "")
	defer dropTables()

	step1Config := fmt.Sprintf(`
resource "postgresql_grant_policy" "test" {
  database = "%[1]s"
  role     = "%[2]s"

  grants = [
    {
      schema      = "test_schema"
      object_type = "table"
      privileges  = ["SELECT"]
    }
  ]
}
`, dbName, roleName)

	// Widens privileges on the table grant and adds a schema-level grant.
	step2Config := fmt.Sprintf(`
resource "postgresql_grant_policy" "test" {
  database = "%[1]s"
  role     = "%[2]s"

  grants = [
    {
      schema      = "test_schema"
      object_type = "table"
      privileges  = ["SELECT", "UPDATE"]
    },
    {
      schema      = "dev_schema"
      object_type = "schema"
      privileges  = ["USAGE"]
    }
  ]
}
`, dbName, roleName)

	// Removes the table grant: in additive (non-authoritative) mode this just
	// stops managing it going forward and revokes exactly what this resource
	// itself granted.
	step3Config := fmt.Sprintf(`
resource "postgresql_grant_policy" "test" {
  database = "%[1]s"
  role     = "%[2]s"

  grants = [
    {
      schema      = "dev_schema"
      object_type = "schema"
      privileges  = ["USAGE"]
    }
  ]
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
				Config: step1Config,
				Check: resource.ComposeTestCheckFunc(
					func(*terraform.State) error {
						return testCheckTablesPrivileges(t, dbName, roleName, tables, []string{"SELECT"})
					},
					resource.TestCheckResourceAttr("postgresql_grant_policy.test", "grants.#", "1"),
				),
			},
			{
				Config: step2Config,
				Check: resource.ComposeTestCheckFunc(
					func(*terraform.State) error {
						return testCheckTablesPrivileges(t, dbName, roleName, tables, []string{"SELECT", "UPDATE"})
					},
					func(*terraform.State) error {
						return testCheckSchemasPrivileges(t, dbName, roleName, []string{"dev_schema"}, []string{"USAGE"})
					},
					resource.TestCheckResourceAttr("postgresql_grant_policy.test", "grants.#", "2"),
				),
			},
			{
				Config: step3Config,
				Check: resource.ComposeTestCheckFunc(
					func(*terraform.State) error {
						return testCheckTablesPrivileges(t, dbName, roleName, tables, []string{})
					},
					resource.TestCheckResourceAttr("postgresql_grant_policy.test", "grants.#", "1"),
				),
			},
		},
	})
}

// Authoritative behavior: with authoritative = false, a grant made outside
// of this resource must survive a re-apply. Flipping to authoritative = true
// must revoke it on the next apply. Covers test matrix items 4, 5, 6
// (preserve unmanaged grants additive, remove unmanaged grants authoritative,
// detect external GRANT).
func TestAccPostgresqlGrantPolicy_Authoritative(t *testing.T) {
	skipIfNotAcc(t)

	dbSuffix, teardown := setupTestDatabase(t, true, true)
	defer teardown()

	config := getTestConfig(t)
	dbName, roleName := getTestDBNames(dbSuffix)

	tables := []string{"test_schema.test_table", "test_schema.other_table"}
	dropTables := createTestTables(t, dbSuffix, tables, "")
	defer dropTables()

	nonAuthoritativeConfig := fmt.Sprintf(`
resource "postgresql_grant_policy" "test" {
  database      = "%[1]s"
  role          = "%[2]s"
  authoritative = false

  grants = [
    {
      schema      = "test_schema"
      object_type = "table"
      objects     = ["test_table"]
      privileges  = ["SELECT"]
    }
  ]
}
`, dbName, roleName)

	// Same as above plus a harmless extra privilege, purely to force
	// Terraform to detect a diff and re-run Create/Update (an unchanged
	// config would produce an empty plan and never re-execute the
	// reconciliation logic at all).
	nonAuthoritativeConfigReapplied := fmt.Sprintf(`
resource "postgresql_grant_policy" "test" {
  database      = "%[1]s"
  role          = "%[2]s"
  authoritative = false

  grants = [
    {
      schema      = "test_schema"
      object_type = "table"
      objects     = ["test_table"]
      privileges  = ["SELECT", "UPDATE"]
    }
  ]
}
`, dbName, roleName)

	authoritativeConfig := fmt.Sprintf(`
resource "postgresql_grant_policy" "test" {
  database      = "%[1]s"
  role          = "%[2]s"
  authoritative = true

  grants = [
    {
      schema      = "test_schema"
      object_type = "table"
      objects     = ["test_table"]
      privileges  = ["SELECT", "UPDATE"]
    }
  ]
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
				// authoritative = false: apply only the declared grant.
				Config: nonAuthoritativeConfig,
				Check: resource.ComposeTestCheckFunc(
					func(*terraform.State) error {
						return testCheckTablesPrivileges(t, dbName, roleName, []string{"test_schema.test_table"}, []string{"SELECT"})
					},
					// Manually grant a privilege on a table this resource never declares.
					func(*terraform.State) error {
						dbExecute(t, config.connStr(dbName), fmt.Sprintf(
							"GRANT SELECT ON test_schema.other_table TO %s", roleName,
						))
						return nil
					},
				),
			},
			{
				// Re-apply with a real (additive) change: the unmanaged grant on
				// other_table must survive since authoritative = false never
				// revokes what it did not declare.
				Config: nonAuthoritativeConfigReapplied,
				Check: resource.ComposeTestCheckFunc(
					func(*terraform.State) error {
						return testCheckTablesPrivileges(t, dbName, roleName, []string{"test_schema.test_table"}, []string{"SELECT", "UPDATE"})
					},
					func(*terraform.State) error {
						return testCheckTablesPrivileges(t, dbName, roleName, []string{"test_schema.other_table"}, []string{"SELECT"})
					},
				),
			},
			{
				// Flip to authoritative = true: other_table is still not declared
				// in `grants`, so its grant must be revoked on this apply.
				Config: authoritativeConfig,
				Check: resource.ComposeTestCheckFunc(
					func(*terraform.State) error {
						return testCheckTablesPrivileges(t, dbName, roleName, []string{"test_schema.other_table"}, []string{})
					},
					func(*terraform.State) error {
						return testCheckTablesPrivileges(t, dbName, roleName, []string{"test_schema.test_table"}, []string{"SELECT", "UPDATE"})
					},
				),
			},
		},
	})
}

// Import: an existing set of grants, created outside of Terraform, can be
// imported and matches what a subsequent apply would produce. Covers test
// matrix item 7 (import).
func TestAccPostgresqlGrantPolicy_Import(t *testing.T) {
	skipIfNotAcc(t)

	dbSuffix, teardown := setupTestDatabase(t, true, true)
	defer teardown()

	dbName, roleName := getTestDBNames(dbSuffix)

	tables := []string{"test_schema.test_table"}
	dropTables := createTestTables(t, dbSuffix, tables, "")
	defer dropTables()

	tfConfig := fmt.Sprintf(`
resource "postgresql_grant_policy" "test" {
  database      = "%[1]s"
  role          = "%[2]s"
  authoritative = false

  grants = [
    {
      schema      = "test_schema"
      object_type = "table"
      privileges  = ["SELECT"]
    }
  ]
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
				Config: tfConfig,
			},
			{
				ResourceName: "postgresql_grant_policy.test",
				ImportState:  true,
				ImportStateIdFunc: func(*terraform.State) (string, error) {
					return fmt.Sprintf("%s/%s", dbName, roleName), nil
				},
				ImportStateVerify: true,
			},
		},
	})
}

// Destroy: additive mode only revokes what was declared (implicit in every
// other test's teardown), and a database-level grant is revoked cleanly on
// destroy. Also covers test matrix item 12 (database-level grants) and,
// via with_grant_option = true, item 10 (grant option).
func TestAccPostgresqlGrantPolicy_DatabaseAndGrantOption(t *testing.T) {
	skipIfNotAcc(t)

	dbSuffix, teardown := setupTestDatabase(t, true, true)
	defer teardown()

	dbName, roleName := getTestDBNames(dbSuffix)

	tfConfig := fmt.Sprintf(`
resource "postgresql_grant_policy" "test" {
  database = "%[1]s"
  role     = "%[2]s"

  grants = [
    {
      object_type = "database"
      privileges  = ["CONNECT"]
    },
    {
      schema            = "test_schema"
      object_type       = "schema"
      privileges        = ["USAGE"]
      with_grant_option = true
    }
  ]
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
				Config: tfConfig,
				Check: resource.ComposeTestCheckFunc(
					testCheckRoleCanConnect(t, dbName, roleName, true),
					resource.TestCheckResourceAttr("postgresql_grant_policy.test", "grants.#", "2"),
					resource.TestCheckResourceAttr("postgresql_grant_policy.test", "grants.1.with_grant_option", "true"),
				),
			},
			{
				Config:  tfConfig,
				Destroy: true,
			},
			{
				// Re-create a role-less config to observe CONNECT was actually
				// revoked on destroy above (the role itself is torn down by
				// setupTestDatabase's deferred teardown, not here).
				Config: fmt.Sprintf(`
resource "postgresql_grant_policy" "test" {
  database = "%[1]s"
  role     = "%[2]s"
  grants   = []
}
`, dbName, roleName),
				Check: testCheckRoleCanConnect(t, dbName, roleName, false),
			},
		},
	})
}

func testCheckRoleCanConnect(t *testing.T, dbName, roleName string, canConnect bool) resource.TestCheckFunc {
	return func(*terraform.State) error {
		config := getTestConfig(t)
		config.Username = roleName
		config.Password = testRolePassword

		db, err := sql.Open("postgres", config.connStr(dbName))
		if err != nil {
			t.Fatalf("could not open connection pool for db %s: %v", dbName, err)
		}
		defer closeDB(t, db)

		err = db.Ping()
		if canConnect && err != nil {
			return fmt.Errorf("expected role %s to be able to connect to %s, got: %w", roleName, dbName, err)
		}
		if !canConnect && err == nil {
			return fmt.Errorf("expected role %s to NOT be able to connect to %s, but it could", roleName, dbName)
		}
		return nil
	}
}
