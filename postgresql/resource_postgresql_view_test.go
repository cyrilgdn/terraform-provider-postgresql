package postgresql

import (
	"database/sql"
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
)

func TestAccPostgresqlView_Basic(t *testing.T) {
	config := `
resource "postgresql_view" "basic_view" {
    name = "basic_view"
    query = "SELECT * FROM tableA"
}
`

	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccPreCheck(t)
			testCheckCompatibleVersion(t, featureFunction)
		},
		Providers:    testAccProviders,
		CheckDestroy: testAccCheckPostgresqlFunctionDestroy,
		Steps: []resource.TestStep{
			{
				Config: config,
				Check: resource.ComposeTestCheckFunc(
					testAccCheckPostgresqlViewExists("postgresql_view.basic_view", ""),
					resource.TestCheckResourceAttr(
						"ppostgresql_view.basic_view", "schema", "public"),
					resource.TestCheckResourceAttr(
						"postgresql_view.basic_view", "name", "basic_view"),
					resource.TestCheckResourceAttr(
						"postgresql_view.basic_view", "query", "SELECT * FROM tableA"),
					resource.TestCheckResourceAttr(
						"postgresql_view.basic_view", "with_check_option", ""),
					resource.TestCheckResourceAttr(
						"postgresql_view.basic_view", "with_security_barrier", "false"),
					resource.TestCheckResourceAttr(
						"postgresql_view.basic_view", "with_security_invoker", "false"),
					resource.TestCheckResourceAttr(
						"postgresql_view.basic_view", "drop_cascade", "false"),
				),
			},
		},
	})
}

func TestAccPostgresqlFunction_SpecificDatabase(t *testing.T) {
	skipIfNotAcc(t)

	dbSuffix, teardown := setupTestDatabase(t, true, true)
	defer teardown()

	dbName, _ := getTestDBNames(dbSuffix)
}

func testAccCheckPostgresqlViewExists(n string, database string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[n]
		if !ok {
			return fmt.Errorf("Resource not found: %s", n)
		}

		if rs.Primary.ID == "" {
			return fmt.Errorf("No ID is set")
		}

		signature := rs.Primary.ID

		client := testAccProvider.Meta().(*Client)
		txn, err := startTransaction(client, database)
		if err != nil {
			return err
		}
		defer deferredRollback(txn)

		exists, err := checkFunctionExists(txn, signature)

		if err != nil {
			return fmt.Errorf("Error checking function %s", err)
		}

		if !exists {
			return fmt.Errorf("Function not found")
		}

		return nil
	}
}

func checkViewExists(txn *sql.Tx, signature string) (bool, error) {
	var _rez bool
	err := txn.QueryRow(fmt.Sprintf("SELECT to_regclass('%s') IS NOT NULL", signature)).Scan(&_rez)
	switch {
	case err == sql.ErrNoRows:
		return false, nil
	case err != nil:
		return false, fmt.Errorf("Error reading info about view: %s", err)
	}

	return _rez, nil
}
