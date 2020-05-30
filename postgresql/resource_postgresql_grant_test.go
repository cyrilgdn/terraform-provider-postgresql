package postgresql

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/helper/schema"
	"github.com/hashicorp/terraform-plugin-sdk/terraform"
	"github.com/lib/pq"
)

func TestCreateGrantQuery(t *testing.T) {
	var databaseName = "foo"
	var roleName = "bar"

	cases := []struct {
		resource   *schema.ResourceData
		privileges []string
		expected   string
	}{
		{
			resource: schema.TestResourceDataRaw(t, resourcePostgreSQLGrant().Schema, map[string]interface{}{
				"object_type": "table",
				"schema":      databaseName,
				"role":        roleName,
			}),
			privileges: []string{"SELECT"},
			expected:   fmt.Sprintf("GRANT SELECT ON ALL TABLES IN SCHEMA %s TO %s", pq.QuoteIdentifier(databaseName), pq.QuoteIdentifier(roleName)),
		},
		{
			resource: schema.TestResourceDataRaw(t, resourcePostgreSQLGrant().Schema, map[string]interface{}{
				"object_type": "sequence",
				"schema":      databaseName,
				"role":        roleName,
			}),
			privileges: []string{"SELECT"},
			expected:   fmt.Sprintf("GRANT SELECT ON ALL SEQUENCES IN SCHEMA %s TO %s", pq.QuoteIdentifier(databaseName), pq.QuoteIdentifier(roleName)),
		},
		{
			resource: schema.TestResourceDataRaw(t, resourcePostgreSQLGrant().Schema, map[string]interface{}{
				"object_type": "function",
				"schema":      databaseName,
				"role":        roleName,
			}),
			privileges: []string{"EXECUTE"},
			expected:   fmt.Sprintf("GRANT EXECUTE ON ALL FUNCTIONS IN SCHEMA %s TO %s", pq.QuoteIdentifier(databaseName), pq.QuoteIdentifier(roleName)),
		},
		{
			resource: schema.TestResourceDataRaw(t, resourcePostgreSQLGrant().Schema, map[string]interface{}{
				"object_type":       "TABLE",
				"schema":            databaseName,
				"role":              roleName,
				"with_grant_option": true,
			}),
			privileges: []string{"SELECT", "INSERT", "UPDATE"},
			expected:   fmt.Sprintf("GRANT SELECT,INSERT,UPDATE ON ALL TABLES IN SCHEMA %s TO %s WITH GRANT OPTION", pq.QuoteIdentifier(databaseName), pq.QuoteIdentifier(roleName)),
		},
		{
			resource: schema.TestResourceDataRaw(t, resourcePostgreSQLGrant().Schema, map[string]interface{}{
				"object_type": "database",
				"database":    databaseName,
				"role":        roleName,
			}),
			privileges: []string{"CREATE"},
			expected:   fmt.Sprintf("GRANT CREATE ON DATABASE %s TO %s", pq.QuoteIdentifier(databaseName), pq.QuoteIdentifier(roleName)),
		},
		{
			resource: schema.TestResourceDataRaw(t, resourcePostgreSQLGrant().Schema, map[string]interface{}{
				"object_type": "database",
				"database":    databaseName,
				"role":        roleName,
			}),
			privileges: []string{"CREATE", "CONNECT"},
			expected:   fmt.Sprintf("GRANT CREATE,CONNECT ON DATABASE %s TO %s", pq.QuoteIdentifier(databaseName), pq.QuoteIdentifier(roleName)),
		},
		{
			resource: schema.TestResourceDataRaw(t, resourcePostgreSQLGrant().Schema, map[string]interface{}{
				"object_type":       "DATABASE",
				"database":          databaseName,
				"role":              roleName,
				"with_grant_option": true,
			}),
			privileges: []string{"ALL PRIVILEGES"},
			expected:   fmt.Sprintf("GRANT ALL PRIVILEGES ON DATABASE %s TO %s WITH GRANT OPTION", pq.QuoteIdentifier(databaseName), pq.QuoteIdentifier(roleName)),
		},
	}

	for _, c := range cases {
		out := createGrantQuery(c.resource, c.privileges)
		if out != c.expected {
			t.Fatalf("Error matching output and expected: %#v vs %#v", out, c.expected)
		}
	}
}

func TestCreateRevokeQuery(t *testing.T) {
	var databaseName = "foo"
	var roleName = "bar"

	cases := []struct {
		resource *schema.ResourceData
		expected string
	}{
		{
			resource: schema.TestResourceDataRaw(t, resourcePostgreSQLGrant().Schema, map[string]interface{}{
				"object_type": "table",
				"schema":      databaseName,
				"role":        roleName,
			}),
			expected: fmt.Sprintf("REVOKE ALL PRIVILEGES ON ALL TABLES IN SCHEMA %s FROM %s", pq.QuoteIdentifier(databaseName), pq.QuoteIdentifier(roleName)),
		},
		{
			resource: schema.TestResourceDataRaw(t, resourcePostgreSQLGrant().Schema, map[string]interface{}{
				"object_type": "sequence",
				"schema":      databaseName,
				"role":        roleName,
			}),
			expected: fmt.Sprintf("REVOKE ALL PRIVILEGES ON ALL SEQUENCES IN SCHEMA %s FROM %s", pq.QuoteIdentifier(databaseName), pq.QuoteIdentifier(roleName)),
		},
		{
			resource: schema.TestResourceDataRaw(t, resourcePostgreSQLGrant().Schema, map[string]interface{}{
				"object_type": "database",
				"database":    databaseName,
				"role":        roleName,
			}),
			expected: fmt.Sprintf("REVOKE ALL PRIVILEGES ON DATABASE %s FROM %s", pq.QuoteIdentifier(databaseName), pq.QuoteIdentifier(roleName)),
		},
		{
			resource: schema.TestResourceDataRaw(t, resourcePostgreSQLGrant().Schema, map[string]interface{}{
				"object_type": "DATABASE",
				"database":    databaseName,
				"role":        roleName,
			}),
			expected: fmt.Sprintf("REVOKE ALL PRIVILEGES ON DATABASE %s FROM %s", pq.QuoteIdentifier(databaseName), pq.QuoteIdentifier(roleName)),
		},
	}

	for _, c := range cases {
		out := createRevokeQuery(c.resource)
		if out != c.expected {
			t.Fatalf("Error matching output and expected: %#v vs %#v", out, c.expected)
		}
	}
}

func TestAccPostgresqlGrant(t *testing.T) {
	skipIfNotAcc(t)

	// We have to create the database outside of resource.Test
	// because we need to create tables to assert that grant are correctly applied
	// and we don't have this resource yet
	dbSuffix, teardown := setupTestDatabase(t, true, true)
	defer teardown()

	testTables := []string{"test_schema.test_table", "test_schema.test_table2"}
	createTestTables(t, dbSuffix, testTables, "")

	dbName, roleName := getTestDBNames(dbSuffix)
	var testGrantSelect = fmt.Sprintf(`
	resource "postgresql_grant" "test" {
		database    = "%s"
		role        = "%s"
		schema      = "test_schema"
		object_type = "table"
		privileges   = ["SELECT"]
	}
	`, dbName, roleName)

	var testGrantSelectInsertUpdate = fmt.Sprintf(`
	resource "postgresql_grant" "test" {
		database    = "%s"
		role        = "%s"
		schema      = "test_schema"
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
					resource.TestCheckResourceAttr(
						"postgresql_grant.test", "id", fmt.Sprintf("%s_%s_test_schema_table", roleName, dbName),
					),
					resource.TestCheckResourceAttr("postgresql_grant.test", "privileges.#", "1"),
					resource.TestCheckResourceAttr("postgresql_grant.test", "privileges.3138006342", "SELECT"),
					func(*terraform.State) error {
						return testCheckTablesPrivileges(t, dbSuffix, testTables, []string{"SELECT"})
					},
				),
			},
			{
				Config: testGrantSelectInsertUpdate,
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("postgresql_grant.test", "privileges.#", "3"),
					resource.TestCheckResourceAttr("postgresql_grant.test", "privileges.3138006342", "SELECT"),
					resource.TestCheckResourceAttr("postgresql_grant.test", "privileges.892623219", "INSERT"),
					resource.TestCheckResourceAttr("postgresql_grant.test", "privileges.1759376126", "UPDATE"),
					func(*terraform.State) error {
						return testCheckTablesPrivileges(t, dbSuffix, testTables, []string{"SELECT", "INSERT", "UPDATE"})
					},
				),
			},
			// Finally reapply the first step to be sure that extra privileges are correctly granted.
			{
				Config: testGrantSelect,
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("postgresql_grant.test", "privileges.#", "1"),
					resource.TestCheckResourceAttr("postgresql_grant.test", "privileges.3138006342", "SELECT"),
					func(*terraform.State) error {
						return testCheckTablesPrivileges(t, dbSuffix, testTables, []string{"SELECT"})
					},
				),
			},
		},
	})
}

func TestAccPostgresqlGrantDatabase(t *testing.T) {
	// create a TF config with placeholder for privileges
	// it will be filled in each step.
	config := fmt.Sprintf(`
resource "postgresql_role" "test" {
	name     = "test_grant_role"
	password = "%s"
	login    = true
}

resource "postgresql_database" "test_db" {
	depends_on = [postgresql_role.test]
	name = "test_grant_db"
}

resource "postgresql_grant" "test" {
	database    = postgresql_database.test_db.name
	role        = postgresql_role.test.name
	object_type = "database"
	privileges  = %%s
}
`, testRolePassword)

	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccPreCheck(t)
			testCheckCompatibleVersion(t, featurePrivileges)
		},
		Providers: testAccProviders,
		Steps: []resource.TestStep{
			// Not allowed to create
			{
				Config: fmt.Sprintf(config, "[\"CONNECT\"]"),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("postgresql_grant.test", "id", "test_grant_role_test_grant_db_database"),
					resource.TestCheckResourceAttr("postgresql_grant.test", "privileges.#", "1"),
					resource.TestCheckResourceAttr("postgresql_grant.test", "with_grant_option", "false"),
					testCheckDatabasesPrivileges(t, false),
				),
			},
			// Can create but not grant
			{
				Config: fmt.Sprintf(config, "[\"CONNECT\", \"CREATE\"]"),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("postgresql_grant.test", "privileges.#", "2"),
					testCheckDatabasesPrivileges(t, true),
				),
			},
		},
	})
}

func testCheckDatabasesPrivileges(t *testing.T, canCreate bool) func(*terraform.State) error {
	return func(*terraform.State) error {
		db := connectAsTestRole(t, "test_grant_role", "test_grant_db")
		defer db.Close()

		if err := testHasGrantForQuery(db, "CREATE SCHEMA plop", canCreate); err != nil {
			return err
		}
		return nil
	}
}
