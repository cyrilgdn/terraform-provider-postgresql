package postgresql

import (
	"database/sql"
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
)

func TestAccPostgresqlProcedure_Basic(t *testing.T) {
	config := `
resource "postgresql_procedure" "basic_procedure" {
    name = "basic_procedure"
    language = "plpgsql"
    body = <<-EOF
        BEGIN
            SELECT 1;
        END;
    EOF
}
`

	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccPreCheck(t)
			testCheckCompatibleVersion(t, featureProcedure)
		},
		Providers:    testAccProviders,
		CheckDestroy: testAccCheckPostgresqlProcedureDestroy,
		Steps: []resource.TestStep{
			{
				Config: config,
				Check: resource.ComposeTestCheckFunc(
					testAccCheckPostgresqlProcedureExists("postgresql_procedure.basic_procedure", ""),
					resource.TestCheckResourceAttr(
						"postgresql_procedure.basic_procedure", "name", "basic_procedure"),
					resource.TestCheckResourceAttr(
						"postgresql_procedure.basic_procedure", "schema", "public"),
					resource.TestCheckResourceAttr(
						"postgresql_procedure.basic_procedure", "language", "plpgsql"),
				),
			},
		},
	})
}

func TestAccPostgresqlProcedure_SpecificDatabase(t *testing.T) {
	skipIfNotAcc(t)

	dbSuffix, teardown := setupTestDatabase(t, true, true)
	defer teardown()

	dbName, _ := getTestDBNames(dbSuffix)

	config := `
resource "postgresql_procedure" "basic_procedure" {
    name = "basic_procedure"
    database = "%s"
    language = "plpgsql"
    body = <<-EOF
        BEGIN
            SELECT 1;
        END;
    EOF
}
`

	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccPreCheck(t)
			testCheckCompatibleVersion(t, featureProcedure)
		},
		Providers:    testAccProviders,
		CheckDestroy: testAccCheckPostgresqlProcedureDestroy,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(config, dbName),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckPostgresqlProcedureExists("postgresql_procedure.basic_procedure", dbName),
					resource.TestCheckResourceAttr(
						"postgresql_procedure.basic_procedure", "name", "basic_procedure"),
					resource.TestCheckResourceAttr(
						"postgresql_procedure.basic_procedure", "database", dbName),
					resource.TestCheckResourceAttr(
						"postgresql_procedure.basic_procedure", "schema", "public"),
					resource.TestCheckResourceAttr(
						"postgresql_procedure.basic_procedure", "language", "plpgsql"),
				),
			},
		},
	})
}

func TestAccPostgresqlProcedure_MultipleArgs(t *testing.T) {
	config := `
resource "postgresql_schema" "test" {
    name = "test"
}

resource "postgresql_procedure" "increment" {
    schema = postgresql_schema.test.name
    name = "increment"
    arg {
        name = "result"
        type = "integer"
        mode = "OUT"
    }
    arg {
        name = "i"
        type = "integer"
        default = "7"
    }
    language = "plpgsql"
    security_definer = true
    body = <<-EOF
        BEGIN
            result = i + 1;
        END;
    EOF
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
					testAccCheckPostgresqlFunctionExists("postgresql_procedure.increment", ""),
					resource.TestCheckResourceAttr(
						"postgresql_procedure.increment", "name", "increment"),
					resource.TestCheckResourceAttr(
						"postgresql_procedure.increment", "schema", "test"),
					resource.TestCheckResourceAttr(
						"postgresql_procedure.increment", "language", "plpgsql"),
					resource.TestCheckResourceAttr(
						"postgresql_procedure.increment", "security_definer", "true"),
				),
			},
		},
	})
}

func TestAccPostgresqlProcedure_Update(t *testing.T) {
	configCreate := `
resource "postgresql_function" "func" {
    name = "func"
    returns = "integer"
    language = "plpgsql"
    body = <<-EOF
        BEGIN
            RETURN 1;
        END;
    EOF
}
`

	configUpdate := `
resource "postgresql_function" "func" {
    name = "func"
    returns = "integer"
    language = "plpgsql"
    volatility = "IMMUTABLE"
    body = <<-EOF
        BEGIN
            RETURN 2;
        END;
    EOF
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
				Config: configCreate,
				Check: resource.ComposeTestCheckFunc(
					testAccCheckPostgresqlFunctionExists("postgresql_function.func", ""),
					resource.TestCheckResourceAttr(
						"postgresql_function.func", "name", "func"),
					resource.TestCheckResourceAttr(
						"postgresql_function.func", "schema", "public"),
					resource.TestCheckResourceAttr(
						"postgresql_function.func", "volatility", "VOLATILE"),
				),
			},
			{
				Config: configUpdate,
				Check: resource.ComposeTestCheckFunc(
					testAccCheckPostgresqlFunctionExists("postgresql_function.func", ""),
					resource.TestCheckResourceAttr(
						"postgresql_function.func", "name", "func"),
					resource.TestCheckResourceAttr(
						"postgresql_function.func", "schema", "public"),
					resource.TestCheckResourceAttr(
						"postgresql_function.func", "volatility", "IMMUTABLE"),
				),
			},
		},
	})
}

func testAccCheckPostgresqlProcedureExists(n string, database string) resource.TestCheckFunc {
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

		exists, err := checkProcedureExists(txn, signature)

		if err != nil {
			return fmt.Errorf("Error checking function %s", err)
		}

		if !exists {
			return fmt.Errorf("Procedure not found")
		}

		return nil
	}
}

func testAccCheckPostgresqlProcedureDestroy(s *terraform.State) error {
	client := testAccProvider.Meta().(*Client)

	for _, rs := range s.RootModule().Resources {
		if rs.Type != "postgresql_procedure" {
			continue
		}

		txn, err := startTransaction(client, "")
		if err != nil {
			return err
		}
		defer deferredRollback(txn)

		_, functionSignature, expandErr := expandProcedureID(rs.Primary.ID, nil, nil)

		if expandErr != nil {
			return fmt.Errorf("Incorrect resource Id %s", err)
		}

		exists, err := checkProcedureExists(txn, functionSignature)

		if err != nil {
			return fmt.Errorf("Error checking procedure %s", err)
		}

		if exists {
			return fmt.Errorf("Procedure still exists after destroy")
		}
	}

	return nil
}

func checkProcedureExists(txn *sql.Tx, signature string) (bool, error) {
	var _rez bool
	err := txn.QueryRow(fmt.Sprintf("SELECT to_regprocedure('%s') IS NOT NULL", signature)).Scan(&_rez)
	switch {
	case err == sql.ErrNoRows:
		return false, nil
	case err != nil:
		return false, fmt.Errorf("Error reading info about procedure: %s", err)
	}

	return _rez, nil
}
