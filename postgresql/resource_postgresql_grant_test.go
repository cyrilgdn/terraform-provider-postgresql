package postgresql

import (
	"fmt"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/helper/schema"
	"github.com/hashicorp/terraform-plugin-sdk/terraform"
	"github.com/lib/pq"
)

func TestCreateGrantQuery(t *testing.T) {
	var databaseName = "foo"
	var roleName = "bar"
	var objects = []interface{}{"o1", "o2"}

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
		{
			resource: schema.TestResourceDataRaw(t, resourcePostgreSQLGrant().Schema, map[string]interface{}{
				"object_type": "table",
				"objects":     objects,
				"schema":      databaseName,
				"role":        roleName,
			}),
			privileges: []string{"SELECT"},
			expected:   fmt.Sprintf(`GRANT SELECT ON TABLE %[1]s."o2",%[1]s."o1" TO %s`, pq.QuoteIdentifier(databaseName), pq.QuoteIdentifier(roleName)),
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
	var objects = []interface{}{"o1", "o2"}

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
		{
			resource: schema.TestResourceDataRaw(t, resourcePostgreSQLGrant().Schema, map[string]interface{}{
				"object_type": "table",
				"objects":     objects,
				"schema":      databaseName,
				"role":        roleName,
			}),
			expected: fmt.Sprintf(`REVOKE ALL PRIVILEGES ON TABLE %[1]s."o2",%[1]s."o1" FROM %s`, pq.QuoteIdentifier(databaseName), pq.QuoteIdentifier(roleName)),
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

	// create a TF config with placeholder for privileges
	// it will be filled in each step.
	var testGrant = fmt.Sprintf(`
	resource "postgresql_grant" "test" {
		database    = "%s"
		role        = "%s"
		schema      = "test_schema"
		object_type = "table"
		privileges   = %%s
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
				Config: fmt.Sprintf(testGrant, `["SELECT"]`),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(
						"postgresql_grant.test", "id", fmt.Sprintf("%s_%s_test_schema_table", roleName, dbName),
					),
					resource.TestCheckResourceAttr("postgresql_grant.test", "privileges.#", "1"),
					resource.TestCheckResourceAttr("postgresql_grant.test", "privileges.3138006342", "SELECT"),
					func(*terraform.State) error {
						return testCheckTablesPrivileges(t, dbName, roleName, testTables, []string{"SELECT"})
					},
				),
			},
			{
				Config: fmt.Sprintf(testGrant, `["SELECT", "INSERT", "UPDATE"]`),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("postgresql_grant.test", "privileges.#", "3"),
					resource.TestCheckResourceAttr("postgresql_grant.test", "privileges.3138006342", "SELECT"),
					resource.TestCheckResourceAttr("postgresql_grant.test", "privileges.892623219", "INSERT"),
					resource.TestCheckResourceAttr("postgresql_grant.test", "privileges.1759376126", "UPDATE"),
					func(*terraform.State) error {
						return testCheckTablesPrivileges(t, dbName, roleName, testTables, []string{"SELECT", "INSERT", "UPDATE"})
					},
				),
			},
			// We reapply the first step to be sure that extra privileges are correctly granted.
			{
				Config: fmt.Sprintf(testGrant, `["SELECT"]`),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("postgresql_grant.test", "privileges.#", "1"),
					resource.TestCheckResourceAttr("postgresql_grant.test", "privileges.3138006342", "SELECT"),
					func(*terraform.State) error {
						return testCheckTablesPrivileges(t, dbName, roleName, testTables, []string{"SELECT"})
					},
				),
			},
			// We test to revoke everything
			{
				Config: fmt.Sprintf(testGrant, `[]`),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("postgresql_grant.test", "privileges.#", "0"),
					func(*terraform.State) error {
						return testCheckTablesPrivileges(t, dbName, roleName, testTables, []string{})
					},
				),
			},
		},
	})
}

func TestAccPostgresqlGrantObjects(t *testing.T) {
	skipIfNotAcc(t)

	dbSuffix, teardown := setupTestDatabase(t, true, true)
	defer teardown()

	testTables := []string{"test_schema.test_table", "test_schema.test_table2"}
	createTestTables(t, dbSuffix, testTables, "")

	dbName, roleName := getTestDBNames(dbSuffix)

	// create a TF config with placeholder for privileges
	// it will be filled in each step.
	var testGrant = fmt.Sprintf(`
	resource "postgresql_grant" "test" {
		database    = "%s"
		role        = "%s"
		schema      = "test_schema"
		object_type = "table"
		objects     = %%s
		privileges  = ["SELECT"]
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
				Config: fmt.Sprintf(testGrant, `["test_table"]`),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(
						"postgresql_grant.test", "id", fmt.Sprintf("%s_%s_test_schema_table_test_table", roleName, dbName),
					),
					resource.TestCheckResourceAttr("postgresql_grant.test", "objects.#", "1"),
					resource.TestCheckResourceAttr("postgresql_grant.test", "objects.4260833613", "test_table"),
					func(*terraform.State) error {
						return testCheckTablesPrivileges(t, dbName, roleName, []string{testTables[0]}, []string{"SELECT"})
					},
					func(*terraform.State) error {
						return testCheckTablesPrivileges(t, dbName, roleName, []string{testTables[1]}, []string{})
					},
				),
			},
			{
				Config: fmt.Sprintf(testGrant, `["test_table", "test_table2"]`),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("postgresql_grant.test", "objects.#", "2"),
					resource.TestCheckResourceAttr("postgresql_grant.test", "objects.4260833613", "test_table"),
					resource.TestCheckResourceAttr("postgresql_grant.test", "objects.306541577", "test_table2"),
					func(*terraform.State) error {
						return testCheckTablesPrivileges(t, dbName, roleName, testTables, []string{"SELECT"})
					},
				),
			},
			{
				Config: fmt.Sprintf(testGrant, `["test_table"]`),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("postgresql_grant.test", "objects.#", "1"),
					resource.TestCheckResourceAttr("postgresql_grant.test", "objects.4260833613", "test_table"),
					func(*terraform.State) error {
						return testCheckTablesPrivileges(t, dbName, roleName, []string{testTables[0]}, []string{"SELECT"})
					},
					func(*terraform.State) error {
						return testCheckTablesPrivileges(t, dbName, roleName, []string{testTables[1]}, []string{})
					},
				),
			},
			{
				// Empty list means that privileges will be applied on all tables.
				Config: fmt.Sprintf(testGrant, `[]`),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("postgresql_grant.test", "objects.#", "0"),
					func(*terraform.State) error {
						return testCheckTablesPrivileges(t, dbName, roleName, testTables, []string{"SELECT"})
					},
				),
			},
			{
				Config:  fmt.Sprintf(testGrant, `[]`),
				Destroy: true,
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("postgresql_grant.test", "objects.#", "0"),
					func(*terraform.State) error {
						return testCheckTablesPrivileges(t, dbName, roleName, testTables, []string{})
					},
				),
			},
		},
	})
}

func TestAccPostgresqlGrantObjectsError(t *testing.T) {
	skipIfNotAcc(t)

	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccPreCheck(t)
			testCheckCompatibleVersion(t, featurePrivileges)
		},
		Providers: testAccProviders,
		Steps: []resource.TestStep{
			{
				Config: `resource "postgresql_grant" "test" {
					database    = "test_db"
					role        = "test_role"
					object_type = "database"
					objects     = ["o1", "o2"]
					privileges  = ["CONNECT"]
				}`,
				ExpectError: regexp.MustCompile("cannot specify `objects` when `object_type` is `database` or `schema`"),
			},
			{
				Config: `resource "postgresql_grant" "test" {
					database    = "test_db"
					schema      = "test_schema"
					role        = "test_role"
					object_type = "schema"
					objects     = ["o1", "o2"]
					privileges  = ["CONNECT"]
				}`,
				ExpectError: regexp.MustCompile("cannot specify `objects` when `object_type` is `database` or `schema`"),
			},
		},
	})
}

func TestAccPostgresqlGrantPublic(t *testing.T) {
	skipIfNotAcc(t)

	config := getTestConfig(t)

	// We have to create the database outside of resource.Test
	// because we need to create tables to assert that grant are correctly applied
	// and we don't have this resource yet
	dbSuffix, teardown := setupTestDatabase(t, true, true)
	defer teardown()

	testTables := []string{"test_schema.test_table"}
	createTestTables(t, dbSuffix, testTables, "")

	dbName, roleName := getTestDBNames(dbSuffix)

	// create another role (first one is created in setupTestDatabase)
	// to assert that PUBLIC is applied to everyone
	role2 := fmt.Sprintf("tf_tests_role2_%s", dbSuffix)
	createTestRole(t, role2)
	dbExecute(t, config.connStr(dbName), fmt.Sprintf("GRANT usage ON SCHEMA test_schema to %s", role2))

	// create a TF config with placeholder for privileges
	// it will be filled in each step.
	var testGrant = fmt.Sprintf(`
	resource "postgresql_grant" "test" {
		database    = "%s"
		role        = "public"
		schema      = "test_schema"
		object_type = "table"
		privileges   = %%s
	}
	`, dbName)

	// Wrapper to testCheckTablesPrivileges to test for both roles
	checkTablePrivileges := func(expectedPrivileges []string) error {
		if err := testCheckTablesPrivileges(t, dbName, roleName, testTables, expectedPrivileges); err != nil {
			return err
		}
		return testCheckTablesPrivileges(t, dbName, role2, testTables, expectedPrivileges)
	}

	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccPreCheck(t)
			testCheckCompatibleVersion(t, featurePrivileges)
		},
		Providers: testAccProviders,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(testGrant, `["SELECT"]`),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(
						"postgresql_grant.test", "id", fmt.Sprintf("public_%s_test_schema_table", dbName),
					),
					resource.TestCheckResourceAttr("postgresql_grant.test", "privileges.#", "1"),
					func(*terraform.State) error {
						return checkTablePrivileges([]string{"SELECT"})
					},
				),
			},
			{
				Config: fmt.Sprintf(testGrant, `["SELECT", "INSERT", "UPDATE"]`),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("postgresql_grant.test", "privileges.#", "3"),
					func(*terraform.State) error {
						return checkTablePrivileges([]string{"SELECT", "INSERT", "UPDATE"})
					},
				),
			},
			// We reapply the first step to be sure that extra privileges are correctly granted.
			{
				Config: fmt.Sprintf(testGrant, `["SELECT"]`),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("postgresql_grant.test", "privileges.#", "1"),
					func(*terraform.State) error {
						return checkTablePrivileges([]string{"SELECT"})
					},
				),
			},
			// We test to revoke everything
			{
				Config: fmt.Sprintf(testGrant, `[]`),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("postgresql_grant.test", "privileges.#", "0"),
					func(*terraform.State) error {
						return checkTablePrivileges([]string{})
					},
				),
			},
		},
	})
}

func TestAccPostgresqlGrantEmptyPrivileges(t *testing.T) {
	skipIfNotAcc(t)

	config := getTestConfig(t)

	dbSuffix, teardown := setupTestDatabase(t, true, true)
	defer teardown()

	testTables := []string{"test_schema.test_table", "test_schema.test_table2"}
	createTestTables(t, dbSuffix, testTables, "")

	dbName, roleName := getTestDBNames(dbSuffix)

	// Grant some privileges on this table to our role to assert that they will be revoked
	dbExecute(t, config.connStr(dbName), fmt.Sprintf("GRANT SELECT, INSERT ON ALL TABLES IN SCHEMA test_schema TO %s", roleName))

	var tfConfig = fmt.Sprintf(`
	resource "postgresql_grant" "test" {
		database    = "%s"
		role        = "%s"
		schema      = "test_schema"
		object_type = "table"
		privileges   = []
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
					resource.TestCheckResourceAttr(
						"postgresql_grant.test", "id", fmt.Sprintf("%s_%s_test_schema_table", roleName, dbName),
					),
					resource.TestCheckResourceAttr("postgresql_grant.test", "privileges.#", "0"),
					func(*terraform.State) error {
						return testCheckTablesPrivileges(t, dbName, roleName, testTables, []string{})
					},
				),
			},
		},
	})
}

func TestAccPostgresqlGrantFunction(t *testing.T) {
	skipIfNotAcc(t)

	config := getTestConfig(t)
	dsn := config.connStr("postgres")

	// Create a test role and a schema as public has too wide open privileges
	dbExecute(t, dsn, fmt.Sprintf("CREATE ROLE test_role LOGIN PASSWORD '%s'", testRolePassword))
	dbExecute(t, dsn, "CREATE SCHEMA test_schema")
	dbExecute(t, dsn, "GRANT USAGE ON SCHEMA test_schema TO test_role")

	// Create test function in this schema
	dbExecute(t, dsn, `
CREATE FUNCTION test_schema.test() RETURNS text
	AS $$ select 'foo'::text $$
    LANGUAGE SQL;
`)
	defer func() {
		dbExecute(t, dsn, "DROP SCHEMA test_schema CASCADE")
		dbExecute(t, dsn, "DROP ROLE test_role")
	}()

	// Test to grant directly to test_role and to public
	// in both case test_case should have the right
	for _, role := range []string{"test_role", "public"} {
		t.Run(role, func(t *testing.T) {

			tfConfig := fmt.Sprintf(`
resource postgresql_grant "test" {
  database    = "postgres"
  role        = "%s"
  schema      = "test_schema"
  object_type = "function"
  privileges  = ["EXECUTE"]
}
	`, role)

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
							resource.TestCheckResourceAttr("postgresql_grant.test", "id", fmt.Sprintf("%s_postgres_test_schema_function", role)),
							resource.TestCheckResourceAttr("postgresql_grant.test", "privileges.#", "1"),
							resource.TestCheckResourceAttr("postgresql_grant.test", "privileges.3223776964", "EXECUTE"),
							resource.TestCheckResourceAttr("postgresql_grant.test", "with_grant_option", "false"),
							testCheckFunctionExecutable(t, "test_role", "test_schema.test"),
						),
					},
				},
			})
		})
	}
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
				Config: fmt.Sprintf(config, `["CONNECT"]`),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("postgresql_grant.test", "id", "test_grant_role_test_grant_db_database"),
					resource.TestCheckResourceAttr("postgresql_grant.test", "privileges.#", "1"),
					resource.TestCheckResourceAttr("postgresql_grant.test", "with_grant_option", "false"),
					testCheckDatabasesPrivileges(t, false),
				),
			},
			// Can create but not grant
			{
				Config: fmt.Sprintf(config, `["CONNECT", "CREATE"]`),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("postgresql_grant.test", "privileges.#", "2"),
					testCheckDatabasesPrivileges(t, true),
				),
			},
			// Revoke
			{
				Config: fmt.Sprintf(config, "[]"),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("postgresql_grant.test", "privileges.#", "0"),
					testCheckDatabasesPrivileges(t, false),
				),
			},
		},
	})
}

func TestAccPostgresqlGrantSchema(t *testing.T) {
	// create a TF config with placeholder for privileges
	// it will be filled in each step.
	config := fmt.Sprintf(`
resource "postgresql_role" "test" {
	name     = "test_grant_role"
	password = "%s"
	login    = true
}

resource "postgresql_schema" "test_schema" {
	depends_on   = [postgresql_role.test]
	name         = "test_schema"
	drop_cascade = true
}

resource "postgresql_grant" "test" {
	database    = "postgres"
	schema      = postgresql_schema.test_schema.name
	role        = postgresql_role.test.name
	object_type = "schema"
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
			{
				Config: fmt.Sprintf(config, `["USAGE"]`),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("postgresql_grant.test", "id", "test_grant_role_postgres_test_schema_schema"),
					resource.TestCheckResourceAttr("postgresql_grant.test", "privileges.#", "1"),
					resource.TestCheckResourceAttr("postgresql_grant.test", "with_grant_option", "false"),
					testCheckSchemaPrivileges(t, true, false),
				),
			},
			{
				Config: fmt.Sprintf(config, `["USAGE", "CREATE"]`),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("postgresql_grant.test", "privileges.#", "2"),
					testCheckSchemaPrivileges(t, true, true),
				),
			},
			{
				//Config: fmt.Sprintf(config, "[]"),
				Config: fmt.Sprintf(config, `[]`),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("postgresql_grant.test", "privileges.#", "0"),
					testCheckSchemaPrivileges(t, false, false),
				),
			},
		},
	})
}

func TestAccPostgresqlGrantForeignDataWrapper(t *testing.T) {
	skipIfNotAcc(t)

	config := getTestConfig(t)
	dsn := config.connStr("postgres")

	// Create a test foreign data wrapper
	dbExecute(t, dsn, "CREATE FOREIGN DATA WRAPPER test_fdw")

	defer func() {
		// Cleanup
		dbExecute(t, dsn, "DROP FOREIGN DATA WRAPPER test_fdw CASCADE")
	}()

	// create a TF config with placeholder for privileges
	// it will be filled in each step.
	tfConfig := fmt.Sprintf(`
resource "postgresql_role" "test" {
	name     = "test_role"
	password = "%s"
	login    = true
}

resource "postgresql_grant" "test" {
	depends_on           = [postgresql_role.test]
	database             = "postgres"
	role                 = postgresql_role.test.name
	object_type          = "foreign_data_wrapper"
	objects              = ["test_fdw"]
	privileges           = %%s
	with_grant_option    = %%s
}
`, testRolePassword)

	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccPreCheck(t)
			testCheckCompatibleVersion(t, featurePrivileges)
		},
		Providers: testAccProviders,
		Steps: []resource.TestStep{
			// Grant usage
			{
				Config: fmt.Sprintf(tfConfig, `["USAGE"]`, `true`),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("postgresql_grant.test", "id", "test_role_postgres_foreign_data_wrapper_test_fdw"),
					resource.TestCheckResourceAttr("postgresql_grant.test", "privileges.#", "1"),
					resource.TestCheckResourceAttr("postgresql_grant.test", "with_grant_option", "true"),
					testCheckForeignDataWrapperPrivileges(t, true),
				),
			},
			// Revoke all privileges
			{
				Config: fmt.Sprintf(tfConfig, `[]`, `false`),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("postgresql_grant.test", "privileges.#", "0"),
					testCheckForeignDataWrapperPrivileges(t, false),
				),
			},
		},
	})
}

func TestAccPostgresqlGrantForeignServer(t *testing.T) {
	skipIfNotAcc(t)

	config := getTestConfig(t)
	dsn := config.connStr("postgres")

	// Create a test foreign data wrapper
	dbExecute(t, dsn, "CREATE FOREIGN DATA WRAPPER test_fdw")
	dbExecute(t, dsn, "CREATE SERVER test_srv FOREIGN DATA WRAPPER test_fdw")

	defer func() {
		// Cleanup
		dbExecute(t, dsn, "DROP FOREIGN DATA WRAPPER test_fdw CASCADE")
	}()

	// create a TF config with placeholder for privileges
	// it will be filled in each step.
	tfConfig := fmt.Sprintf(`
resource "postgresql_role" "test" {
	name     = "test_role"
	password = "%s"
	login    = true
}

resource "postgresql_grant" "test" {
	depends_on        = [postgresql_role.test]
	database          = "postgres"
	role              = postgresql_role.test.name
	object_type       = "foreign_server"
	objects           = ["test_srv"]
	privileges        = %%s
	with_grant_option = %%s
}
`, testRolePassword)

	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccPreCheck(t)
			testCheckCompatibleVersion(t, featurePrivileges)
		},
		Providers: testAccProviders,
		Steps: []resource.TestStep{
			// Grant usage
			{
				Config: fmt.Sprintf(tfConfig, `["USAGE"]`, `false`),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("postgresql_grant.test", "id", "test_role_postgres_foreign_server_test_srv"),
					resource.TestCheckResourceAttr("postgresql_grant.test", "privileges.#", "1"),
					resource.TestCheckResourceAttr("postgresql_grant.test", "with_grant_option", "false"),
					testCheckForeignServerPrivileges(t, true),
				),
			},
			// Revoke all privileges
			{
				Config: fmt.Sprintf(tfConfig, `[]`, `false`),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("postgresql_grant.test", "privileges.#", "0"),
					testCheckForeignServerPrivileges(t, false),
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

func testCheckFunctionExecutable(t *testing.T, role, function string) func(*terraform.State) error {
	return func(*terraform.State) error {
		db := connectAsTestRole(t, role, "postgres")
		defer db.Close()

		if err := testHasGrantForQuery(db, fmt.Sprintf("SELECT %s()", function), true); err != nil {
			return err
		}
		return nil
	}
}

func testCheckSchemaPrivileges(t *testing.T, usage, create bool) func(*terraform.State) error {
	return func(*terraform.State) error {
		config := getTestConfig(t)
		dsn := config.connStr("postgres")

		// Create a table in the schema to check if user has usage privilege
		dbExecute(t, dsn, "CREATE TABLE IF NOT EXISTS test_schema.test_usage (id serial)")
		defer func() {
			dbExecute(t, dsn, "DROP TABLE IF EXISTS test_schema.test_create")
		}()
		dbExecute(t, dsn, "GRANT SELECT ON test_schema.test_usage TO test_grant_role")

		db := connectAsTestRole(t, "test_grant_role", "postgres")
		defer db.Close()

		if err := testHasGrantForQuery(db, "SELECT 1 FROM test_schema.test_usage", usage); err != nil {
			return err
		}

		if err := testHasGrantForQuery(db, "CREATE TABLE test_schema.test_create (id serial)", create); err != nil {
			return err
		}

		return nil
	}
}

func testCheckForeignDataWrapperPrivileges(t *testing.T, usage bool) func(*terraform.State) error {
	return func(*terraform.State) error {
		config := getTestConfig(t)
		dsn := config.connStr("postgres")

		defer func() {
			dbExecute(t, dsn, "DROP SERVER IF EXISTS test_srv")
		}()
		db := connectAsTestRole(t, "test_role", "postgres")
		defer db.Close()

		if err := testHasGrantForQuery(db, "CREATE SERVER test_srv FOREIGN DATA WRAPPER test_fdw", usage); err != nil {
			return err
		}

		return nil
	}
}

func testCheckForeignServerPrivileges(t *testing.T, usage bool) func(*terraform.State) error {
	return func(*terraform.State) error {
		config := getTestConfig(t)
		dsn := config.connStr("postgres")

		defer func() {
			dbExecute(t, dsn, "DROP FOREIGN TABLE IF EXISTS test_tbl")
		}()
		db := connectAsTestRole(t, "test_role", "postgres")
		defer db.Close()

		if err := testHasGrantForQuery(db, "CREATE FOREIGN TABLE test_tbl() SERVER test_srv", usage); err != nil {
			return err
		}

		return nil
	}
}
