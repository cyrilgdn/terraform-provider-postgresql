package postgresql

import (
	"database/sql"
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/terraform"
)

func TestAccPostgresqlSchema_Basic(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:     func() { testAccPreCheck(t) },
		Providers:    testAccProviders,
		CheckDestroy: testAccCheckPostgresqlSchemaDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccPostgresqlSchemaConfig,
				Check: resource.ComposeTestCheckFunc(
					testAccCheckPostgresqlSchemaExists("postgresql_schema.test1", "foo"),
					resource.TestCheckResourceAttr("postgresql_role.role_all_without_grant", "name", "role_all_without_grant"),
					resource.TestCheckResourceAttr("postgresql_role.role_all_without_grant", "login", "true"),

					resource.TestCheckResourceAttr("postgresql_role.role_all_with_grant", "name", "role_all_with_grant"),

					resource.TestCheckResourceAttr("postgresql_schema.test1", "name", "foo"),

					resource.TestCheckResourceAttr("postgresql_schema.test2", "name", "bar"),
					resource.TestCheckResourceAttr("postgresql_schema.test2", "owner", "role_all_without_grant"),
					resource.TestCheckResourceAttr("postgresql_schema.test2", "if_not_exists", "false"),
					resource.TestCheckResourceAttr("postgresql_schema.test2", "policy.#", "1"),
					resource.TestCheckResourceAttr("postgresql_schema.test2", "policy.1948480595.create", "true"),
					resource.TestCheckResourceAttr("postgresql_schema.test2", "policy.1948480595.create_with_grant", "false"),
					resource.TestCheckResourceAttr("postgresql_schema.test2", "policy.1948480595.usage", "true"),
					resource.TestCheckResourceAttr("postgresql_schema.test2", "policy.1948480595.usage_with_grant", "false"),
					resource.TestCheckResourceAttr("postgresql_schema.test2", "policy.1948480595.role", "role_all_without_grant"),

					resource.TestCheckResourceAttr("postgresql_schema.test3", "name", "baz"),
					resource.TestCheckResourceAttr("postgresql_schema.test3", "owner", "role_all_without_grant"),
					resource.TestCheckResourceAttr("postgresql_schema.test3", "if_not_exists", "true"),
					resource.TestCheckResourceAttr("postgresql_schema.test3", "policy.#", "2"),
					resource.TestCheckResourceAttr("postgresql_schema.test3", "policy.1013320538.create_with_grant", "true"),
					resource.TestCheckResourceAttr("postgresql_schema.test3", "policy.1013320538.usage_with_grant", "true"),
					resource.TestCheckResourceAttr("postgresql_schema.test3", "policy.1013320538.role", "role_all_with_grant"),
					resource.TestCheckResourceAttr("postgresql_schema.test3", "policy.1948480595.create", "true"),
					resource.TestCheckResourceAttr("postgresql_schema.test3", "policy.1948480595.usage", "true"),
					resource.TestCheckResourceAttr("postgresql_schema.test3", "policy.1948480595.role", "role_all_without_grant"),
				),
			},
		},
	})
}

func TestAccPostgresqlSchema_AddPolicy(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccPreCheck(t)
			// TODO: Need to check if remooving policy is buggy
			// because non-superuser fails to drop a role
			testSuperuserPreCheck(t)
		},
		Providers:    testAccProviders,
		CheckDestroy: testAccCheckPostgresqlSchemaDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccPostgresqlSchemaGrant1,
				Check: resource.ComposeTestCheckFunc(
					testAccCheckPostgresqlSchemaExists("postgresql_schema.test4", "test4"),

					resource.TestCheckResourceAttr("postgresql_role.all_without_grant_stay", "name", "all_without_grant_stay"),
					resource.TestCheckResourceAttr("postgresql_role.all_without_grant_drop", "name", "all_without_grant_drop"),
					resource.TestCheckResourceAttr("postgresql_role.policy_compose", "name", "policy_compose"),
					resource.TestCheckResourceAttr("postgresql_role.policy_move", "name", "policy_move"),

					resource.TestCheckResourceAttr("postgresql_role.all_with_grantstay", "name", "all_with_grantstay"),
					resource.TestCheckResourceAttr("postgresql_role.all_with_grantdrop", "name", "all_with_grantdrop"),

					resource.TestCheckResourceAttr("postgresql_schema.test4", "name", "test4"),
					resource.TestCheckResourceAttr("postgresql_schema.test4", "owner", "all_without_grant_stay"),
					resource.TestCheckResourceAttr("postgresql_schema.test4", "policy.#", "7"),
					resource.TestCheckResourceAttr("postgresql_schema.test4", "policy.108605972.create", "false"),
					resource.TestCheckResourceAttr("postgresql_schema.test4", "policy.108605972.create_with_grant", "true"),
					resource.TestCheckResourceAttr("postgresql_schema.test4", "policy.108605972.role", "all_with_grantstay"),
					resource.TestCheckResourceAttr("postgresql_schema.test4", "policy.108605972.usage", "false"),
					resource.TestCheckResourceAttr("postgresql_schema.test4", "policy.108605972.usage_with_grant", "true"),
					resource.TestCheckResourceAttr("postgresql_schema.test4", "policy.1417738359.create", "true"),
					resource.TestCheckResourceAttr("postgresql_schema.test4", "policy.1417738359.create_with_grant", "false"),
					resource.TestCheckResourceAttr("postgresql_schema.test4", "policy.1417738359.role", "policy_move"),
					resource.TestCheckResourceAttr("postgresql_schema.test4", "policy.1417738359.usage", "true"),
					resource.TestCheckResourceAttr("postgresql_schema.test4", "policy.1417738359.usage_with_grant", "false"),
					resource.TestCheckResourceAttr("postgresql_schema.test4", "policy.1762357194.create", "true"),
					resource.TestCheckResourceAttr("postgresql_schema.test4", "policy.1762357194.create_with_grant", "false"),
					resource.TestCheckResourceAttr("postgresql_schema.test4", "policy.1762357194.role", "all_without_grant_drop"),
					resource.TestCheckResourceAttr("postgresql_schema.test4", "policy.1762357194.usage", "true"),
					resource.TestCheckResourceAttr("postgresql_schema.test4", "policy.1762357194.usage_with_grant", "false"),
					resource.TestCheckResourceAttr("postgresql_schema.test4", "policy.2524457447.create", "true"),
					resource.TestCheckResourceAttr("postgresql_schema.test4", "policy.2524457447.create_with_grant", "false"),
					resource.TestCheckResourceAttr("postgresql_schema.test4", "policy.2524457447.role", "all_without_grant_stay"),
					resource.TestCheckResourceAttr("postgresql_schema.test4", "policy.2524457447.usage", "true"),
					resource.TestCheckResourceAttr("postgresql_schema.test4", "policy.2524457447.usage_with_grant", "false"),
					resource.TestCheckResourceAttr("postgresql_schema.test4", "policy.3959936977.create", "false"),
					resource.TestCheckResourceAttr("postgresql_schema.test4", "policy.3959936977.create_with_grant", "true"),
					resource.TestCheckResourceAttr("postgresql_schema.test4", "policy.3959936977.role", "policy_compose"),
					resource.TestCheckResourceAttr("postgresql_schema.test4", "policy.3959936977.usage", "false"),
					resource.TestCheckResourceAttr("postgresql_schema.test4", "policy.3959936977.usage_with_grant", "true"),
					resource.TestCheckResourceAttr("postgresql_schema.test4", "policy.4178211897.create", "false"),
					resource.TestCheckResourceAttr("postgresql_schema.test4", "policy.4178211897.create_with_grant", "true"),
					resource.TestCheckResourceAttr("postgresql_schema.test4", "policy.4178211897.role", "all_with_grantdrop"),
					resource.TestCheckResourceAttr("postgresql_schema.test4", "policy.4178211897.usage", "false"),
					resource.TestCheckResourceAttr("postgresql_schema.test4", "policy.4178211897.usage_with_grant", "true"),
					resource.TestCheckResourceAttr("postgresql_schema.test4", "policy.815478369.create", "true"),
					resource.TestCheckResourceAttr("postgresql_schema.test4", "policy.815478369.create_with_grant", "false"),
					resource.TestCheckResourceAttr("postgresql_schema.test4", "policy.815478369.role", "policy_compose"),
					resource.TestCheckResourceAttr("postgresql_schema.test4", "policy.815478369.usage", "true"),
					resource.TestCheckResourceAttr("postgresql_schema.test4", "policy.815478369.usage_with_grant", "false"),
				),
			},
			{
				Config: testAccPostgresqlSchemaGrant2,
				Check: resource.ComposeTestCheckFunc(
					testAccCheckPostgresqlSchemaExists("postgresql_schema.test4", "test4"),
					resource.TestCheckResourceAttr("postgresql_role.all_without_grant_stay", "name", "all_without_grant_stay"),
					resource.TestCheckResourceAttr("postgresql_role.all_without_grant_drop", "name", "all_without_grant_drop"),
					resource.TestCheckResourceAttr("postgresql_role.policy_compose", "name", "policy_compose"),
					resource.TestCheckResourceAttr("postgresql_role.policy_move", "name", "policy_move"),

					resource.TestCheckResourceAttr("postgresql_role.all_with_grantstay", "name", "all_with_grantstay"),

					resource.TestCheckResourceAttr("postgresql_schema.test4", "name", "test4"),
					resource.TestCheckResourceAttr("postgresql_schema.test4", "owner", "all_without_grant_stay"),
					resource.TestCheckResourceAttr("postgresql_schema.test4", "policy.#", "6"),
					resource.TestCheckResourceAttr("postgresql_schema.test4", "policy.108605972.create", "false"),
					resource.TestCheckResourceAttr("postgresql_schema.test4", "policy.108605972.create_with_grant", "true"),
					resource.TestCheckResourceAttr("postgresql_schema.test4", "policy.108605972.role", "all_with_grantstay"),
					resource.TestCheckResourceAttr("postgresql_schema.test4", "policy.108605972.usage", "false"),
					resource.TestCheckResourceAttr("postgresql_schema.test4", "policy.108605972.usage_with_grant", "true"),
					resource.TestCheckResourceAttr("postgresql_schema.test4", "policy.2524457447.create", "true"),
					resource.TestCheckResourceAttr("postgresql_schema.test4", "policy.2524457447.create_with_grant", "false"),
					resource.TestCheckResourceAttr("postgresql_schema.test4", "policy.2524457447.role", "all_without_grant_stay"),
					resource.TestCheckResourceAttr("postgresql_schema.test4", "policy.2524457447.usage", "true"),
					resource.TestCheckResourceAttr("postgresql_schema.test4", "policy.2524457447.usage_with_grant", "false"),
					resource.TestCheckResourceAttr("postgresql_schema.test4", "policy.3831594020.create", "false"),
					resource.TestCheckResourceAttr("postgresql_schema.test4", "policy.3831594020.create_with_grant", "true"),
					resource.TestCheckResourceAttr("postgresql_schema.test4", "policy.3831594020.role", "policy_move"),
					resource.TestCheckResourceAttr("postgresql_schema.test4", "policy.3831594020.usage", "false"),
					resource.TestCheckResourceAttr("postgresql_schema.test4", "policy.3831594020.usage_with_grant", "true"),
					resource.TestCheckResourceAttr("postgresql_schema.test4", "policy.3959936977.create", "false"),
					resource.TestCheckResourceAttr("postgresql_schema.test4", "policy.3959936977.create_with_grant", "true"),
					resource.TestCheckResourceAttr("postgresql_schema.test4", "policy.3959936977.role", "policy_compose"),
					resource.TestCheckResourceAttr("postgresql_schema.test4", "policy.3959936977.usage", "false"),
					resource.TestCheckResourceAttr("postgresql_schema.test4", "policy.3959936977.usage_with_grant", "true"),
					resource.TestCheckResourceAttr("postgresql_schema.test4", "policy.468685299.create", "true"),
					resource.TestCheckResourceAttr("postgresql_schema.test4", "policy.468685299.create_with_grant", "false"),
					resource.TestCheckResourceAttr("postgresql_schema.test4", "policy.468685299.role", "policy_new"),
					resource.TestCheckResourceAttr("postgresql_schema.test4", "policy.468685299.usage", "true"),
					resource.TestCheckResourceAttr("postgresql_schema.test4", "policy.468685299.usage_with_grant", "false"),
					resource.TestCheckResourceAttr("postgresql_schema.test4", "policy.815478369.create", "true"),
					resource.TestCheckResourceAttr("postgresql_schema.test4", "policy.815478369.create_with_grant", "false"),
					resource.TestCheckResourceAttr("postgresql_schema.test4", "policy.815478369.role", "policy_compose"),
					resource.TestCheckResourceAttr("postgresql_schema.test4", "policy.815478369.usage", "true"),
					resource.TestCheckResourceAttr("postgresql_schema.test4", "policy.815478369.usage_with_grant", "false"),
				),
			},
		},
	})
}

func TestAccPostgresqlSchema_Database(t *testing.T) {
	skipIfNotAcc(t)

	dbSuffix, teardown := setupTestDatabase(t, true, true)
	defer teardown()

	dbName, _ := getTestDBNames(dbSuffix)

	testAccPostgresqlSchemaDatabaseConfig := fmt.Sprintf(`
	resource "postgresql_schema" "test_database" {
		name     = "test_database"
		database = "%s"
	}
	`, dbName)

	resource.Test(t, resource.TestCase{
		PreCheck:     func() { testAccPreCheck(t) },
		Providers:    testAccProviders,
		CheckDestroy: testAccCheckPostgresqlSchemaDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccPostgresqlSchemaDatabaseConfig,
				Check: resource.ComposeTestCheckFunc(
					testAccCheckPostgresqlSchemaExists("postgresql_schema.test_database", "test_database"),
					resource.TestCheckResourceAttr(
						"postgresql_schema.test_database", "name", "test_database"),
					resource.TestCheckResourceAttr(
						"postgresql_schema.test_database", "database", dbName),
				),
			},
		},
	})
}

func TestAccPostgresqlSchema_DropCascade(t *testing.T) {
	skipIfNotAcc(t)

	dbSuffix, teardown := setupTestDatabase(t, true, true)
	defer teardown()

	dbName, _ := getTestDBNames(dbSuffix)

	var testAccPostgresqlSchemaConfig = fmt.Sprintf(`
resource "postgresql_schema" "test_cascade" {
  name = "foo"
  database = "%s"
  drop_cascade = true
}
`, dbName)
	resource.Test(t, resource.TestCase{
		PreCheck:     func() { testAccPreCheck(t) },
		Providers:    testAccProviders,
		CheckDestroy: testAccCheckPostgresqlSchemaDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccPostgresqlSchemaConfig,
				Check: resource.ComposeTestCheckFunc(
					testAccCheckPostgresqlSchemaExists("postgresql_schema.test_cascade", "foo"),
					resource.TestCheckResourceAttr("postgresql_schema.test_cascade", "name", "foo"),

					// This will create a table in the schema to check if the drop will work thanks to the cascade
					testAccCreateSchemaTable(dbName, "foo"),
				),
			},
		},
	})
}

func testAccCheckPostgresqlSchemaDestroy(s *terraform.State) error {
	client := testAccProvider.Meta().(*Client)

	for _, rs := range s.RootModule().Resources {
		if rs.Type != "postgresql_schema" {
			continue
		}

		database, ok := rs.Primary.Attributes[schemaDatabaseAttr]
		if !ok {
			return fmt.Errorf("No Attribute for database is set")
		}

		txn, err := startTransaction(client, database)
		if err != nil {
			return err
		}
		defer deferredRollback(txn)

		exists, err := checkSchemaExists(txn, getExtensionNameFromID(rs.Primary.ID))

		if err != nil {
			return fmt.Errorf("Error checking schema %s", err)
		}

		if exists {
			return fmt.Errorf("Schema still exists after destroy")
		}
	}

	return nil
}

func testAccCheckPostgresqlSchemaExists(n string, schemaName string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[n]
		if !ok {
			return fmt.Errorf("Resource not found: %s", n)
		}

		if rs.Primary.ID == "" {
			return fmt.Errorf("No ID is set")
		}

		database, ok := rs.Primary.Attributes[schemaDatabaseAttr]
		if !ok {
			return fmt.Errorf("No Attribute for database is set")
		}

		actualSchemaName := rs.Primary.Attributes["name"]
		if actualSchemaName != schemaName {
			return fmt.Errorf("Wrong value for schema name expected %s got %s", schemaName, actualSchemaName)
		}

		client := testAccProvider.Meta().(*Client)
		txn, err := startTransaction(client, database)
		if err != nil {
			return err
		}
		defer deferredRollback(txn)

		exists, err := checkSchemaExists(txn, schemaName)

		if err != nil {
			return fmt.Errorf("Error checking schema %s", err)
		}

		if !exists {
			return fmt.Errorf("Schema not found")
		}

		return nil
	}
}

func checkSchemaExists(txn *sql.Tx, schemaName string) (bool, error) {
	var _rez bool
	err := txn.QueryRow("SELECT TRUE FROM pg_catalog.pg_namespace WHERE nspname=$1", schemaName).Scan(&_rez)
	switch {
	case err == sql.ErrNoRows:
		return false, nil
	case err != nil:
		return false, fmt.Errorf("error reading info about schema: %w", err)
	}

	return true, nil
}

func testAccCreateSchemaTable(database, schemaName string) resource.TestCheckFunc {
	return func(s *terraform.State) error {

		client, err := testAccProvider.Meta().(*Client).config.NewClient(database)
		if err != nil {
			return fmt.Errorf("could not create client on database %s: %w", schemaName, err)
		}
		db := client.DB()

		if _, err = db.Exec(fmt.Sprintf("CREATE TABLE %s.test_table (id serial)", schemaName)); err != nil {
			return fmt.Errorf("could not create test table in schema %s: %s", schemaName, err)
		}

		return nil
	}
}

const testAccPostgresqlSchemaConfig = `
resource "postgresql_role" "role_all_without_grant" {
  name = "role_all_without_grant"
  login = true
}

resource "postgresql_role" "role_all_with_grant" {
  name = "role_all_with_grant"
}

resource "postgresql_schema" "test1" {
  name = "foo"
}

resource "postgresql_schema" "test2" {
  name = "bar"
  owner = "${postgresql_role.role_all_without_grant.name}"
  if_not_exists = false

  policy {
    create = true
    usage = true
    role = "${postgresql_role.role_all_without_grant.name}"
  }
}

resource "postgresql_schema" "test3" {
  name = "baz"
  owner = "${postgresql_role.role_all_without_grant.name}"
  if_not_exists = true

  policy {
    create_with_grant = true
    usage_with_grant = true
    role = "${postgresql_role.role_all_with_grant.name}"
  }

  policy {
    create = true
    usage = true
    role = "${postgresql_role.role_all_without_grant.name}"
  }
}
`

const testAccPostgresqlSchemaGrant1 = `
resource "postgresql_role" "all_without_grant_stay" {
  name = "all_without_grant_stay"
}

resource "postgresql_role" "all_without_grant_drop" {
  name = "all_without_grant_drop"
}

resource "postgresql_role" "policy_compose" {
  name = "policy_compose"
}

resource "postgresql_role" "policy_move" {
  name = "policy_move"
}

resource "postgresql_role" "all_with_grantstay" {
  name = "all_with_grantstay"
}

resource "postgresql_role" "all_with_grantdrop" {
  name = "all_with_grantdrop"
}

resource "postgresql_schema" "test4" {
  name = "test4"
  owner = "${postgresql_role.all_without_grant_stay.name}"

  policy {
    create = true
    usage = true
    role = "${postgresql_role.all_without_grant_stay.name}"
  }

  policy {
    create = true
    usage = true
    role = "${postgresql_role.all_without_grant_drop.name}"
  }

  policy {
    create = true
    usage = true
    role = "${postgresql_role.policy_compose.name}"
  }

  policy {
    create = true
    usage = true
    role = "${postgresql_role.policy_move.name}"
  }

  policy {
    create_with_grant = true
    usage_with_grant = true
    role = "${postgresql_role.all_with_grantstay.name}"
  }

  policy {
    create_with_grant = true
    usage_with_grant = true
    role = "${postgresql_role.all_with_grantdrop.name}"
  }

  policy {
    create_with_grant = true
    usage_with_grant = true
    role = "${postgresql_role.policy_compose.name}"
  }
}
`

const testAccPostgresqlSchemaGrant2 = `
resource "postgresql_role" "all_without_grant_stay" {
  name = "all_without_grant_stay"
}

resource "postgresql_role" "all_without_grant_drop" {
  name = "all_without_grant_drop"
}

resource "postgresql_role" "policy_compose" {
  name = "policy_compose"
}

resource "postgresql_role" "policy_move" {
  name = "policy_move"
}

resource "postgresql_role" "all_with_grantstay" {
  name = "all_with_grantstay"
}

resource "postgresql_role" "policy_new" {
  name = "policy_new"
}

resource "postgresql_schema" "test4" {
  name = "test4"
  owner = "${postgresql_role.all_without_grant_stay.name}"

  policy {
    create = true
    usage = true
    role = "${postgresql_role.all_without_grant_stay.name}"
  }

  policy {
    create = true
    usage = true
    role = "${postgresql_role.policy_compose.name}"
  }

  policy {
    create_with_grant = true
    usage_with_grant = true
    role = "${postgresql_role.all_with_grantstay.name}"
  }

  policy {
    create_with_grant = true
    usage_with_grant = true
    role = "${postgresql_role.policy_compose.name}"
  }

  policy {
    create_with_grant = true
    usage_with_grant = true
    role = "${postgresql_role.policy_move.name}"
  }

  policy {
    create = true
    usage = true
    role = "${postgresql_role.policy_new.name}"
  }
}
`
