package postgresql

import (
	"database/sql"
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
)

func TestAccPostgresqlEventTrigger_Basic(t *testing.T) {
	skipIfNotAcc(t)
	testSuperuserPreCheck(t)

	// Create the database outside of resource.Test
	// because we need to create test schemas.
	dbSuffix, teardown := setupTestDatabase(t, true, true)
	defer teardown()

	schemas := []string{"test_schema1"}
	createTestSchemas(t, dbSuffix, schemas, "")

	dbName, _ := getTestDBNames(dbSuffix)

	testAccPostgresqlDataSourceTablesEventTriggerConfig := fmt.Sprintf(testAccPostgreSQLEventTriggerConfig, dbName, schemas[0])

	resource.Test(t, resource.TestCase{
		PreCheck:     func() { testAccPreCheck(t) },
		Providers:    testAccProviders,
		CheckDestroy: testAccCheckPostgresqlEventTriggerDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccPostgresqlDataSourceTablesEventTriggerConfig,
				Check: resource.ComposeTestCheckFunc(
					testAccCheckPostgresqlEventTriggerExists("postgresql_event_trigger.event_trigger", dbName),
				),
			},
		},
	})
}

func testAccCheckPostgresqlEventTriggerExists(n string, database string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[n]
		if !ok {
			return fmt.Errorf("Resource not found: %s", n)
		}

		if rs.Primary.ID == "" {
			return fmt.Errorf("No ID is set")
		}

		client := testAccProvider.Meta().(*Client)
		txn, err := startTransaction(client, database)
		if err != nil {
			return err
		}
		defer deferredRollback(txn)

		exists, err := checkEventTriggerExists(txn, rs.Primary.ID)

		if err != nil {
			return fmt.Errorf("Error checking event trigger %s", err)
		}

		if !exists {
			return fmt.Errorf("Event trigger not found")
		}

		return nil
	}
}

func testAccCheckPostgresqlEventTriggerDestroy(s *terraform.State) error {
	client := testAccProvider.Meta().(*Client)

	for _, rs := range s.RootModule().Resources {
		if rs.Type != "postgresql_event_trigger" {
			continue
		}

		var database string
		for k, v := range rs.Primary.Attributes {
			if k == "database" {
				database = v
			}
		}

		txn, err := startTransaction(client, database)
		if err != nil {
			return err
		}
		defer deferredRollback(txn)

		exists, err := checkEventTriggerExists(txn, rs.Primary.ID)

		if err != nil {
			return fmt.Errorf("Error checking event trigger %s", err)
		}

		if exists {
			return fmt.Errorf("Event trigger still exists after destroy")
		}
	}

	return nil
}

func checkEventTriggerExists(txn *sql.Tx, signature string) (bool, error) {
	var _rez string
	err := txn.QueryRow("SELECT oid FROM pg_catalog.pg_event_trigger WHERE evtname=$1", signature).Scan(&_rez)
	switch {
	case err == sql.ErrNoRows:
		return false, nil
	case err != nil:
		return false, fmt.Errorf("Error reading info about event trigger: %s", err)
	}

	return true, nil
}

var testAccPostgreSQLEventTriggerConfig = `
resource "postgresql_function" "function" {
    name = "test_function"
	database = "%[1]s"
	schema = "%[2]s"

    returns = "event_trigger"
    language = "plpgsql"
    body = <<-EOF
        BEGIN
            RAISE EXCEPTION 'command % is disabled', tg_tag;
        END;
    EOF
}

resource "postgresql_event_trigger" "event_trigger" {
  name = "event_trigger_test"
  database = "%[1]s"
  function = postgresql_function.function.name
  function_schema = postgresql_function.function.schema
  on = "ddl_command_end"
  owner = "postgres"
  status = "enable"

  filter {
    variable = "TAG"
    values = [
      "CREATE TABLE",
      "ALTER TABLE",
    ]
  }
}
`
