package postgresql

import (
	"database/sql"
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
)

func TestAccPostgresqlFunction_Basic(t *testing.T) {
	config := `
resource "postgresql_function" "basic_function" {
    name = "basic_function"
    returns = "integer"
    language = "plpgsql"
    body = <<-EOF
        BEGIN
            RETURN 1;
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
					testAccCheckPostgresqlFunctionExists("postgresql_function.basic_function", ""),
					resource.TestCheckResourceAttr(
						"postgresql_function.basic_function", "name", "basic_function"),
					resource.TestCheckResourceAttr(
						"postgresql_function.basic_function", "schema", "public"),
					resource.TestCheckResourceAttr(
						"postgresql_function.basic_function", "language", "plpgsql"),
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

	config := `
resource "postgresql_function" "basic_function" {
    name = "basic_function"
    database = "%s"
    returns = "integer"
    language = "plpgsql"
    body = <<-EOF
        BEGIN
            RETURN 1;
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
				Config: fmt.Sprintf(config, dbName),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckPostgresqlFunctionExists("postgresql_function.basic_function", dbName),
					resource.TestCheckResourceAttr(
						"postgresql_function.basic_function", "name", "basic_function"),
					resource.TestCheckResourceAttr(
						"postgresql_function.basic_function", "database", dbName),
					resource.TestCheckResourceAttr(
						"postgresql_function.basic_function", "schema", "public"),
					resource.TestCheckResourceAttr(
						"postgresql_function.basic_function", "language", "plpgsql"),
				),
			},
		},
	})
}

func TestAccPostgresqlFunction_MultipleArgs(t *testing.T) {
	config := `
resource "postgresql_schema" "test" {
    name = "test"
}

resource "postgresql_function" "increment" {
    schema = postgresql_schema.test.name
    name = "increment"
    arg {
        name = "i"
        type = "integer"
        default = "7"
    }
    arg {
        name = "result"
        type = "integer"
        mode = "OUT"
    }
    language = "plpgsql"
    parallel = "RESTRICTED"
    strict = true
    security_definer = true
    volatility = "STABLE"
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
					testAccCheckPostgresqlFunctionExists("postgresql_function.increment", ""),
					resource.TestCheckResourceAttr(
						"postgresql_function.increment", "name", "increment"),
					resource.TestCheckResourceAttr(
						"postgresql_function.increment", "schema", "test"),
					resource.TestCheckResourceAttr(
						"postgresql_function.increment", "language", "plpgsql"),
					resource.TestCheckResourceAttr(
						"postgresql_function.increment", "strict", "true"),
					resource.TestCheckResourceAttr(
						"postgresql_function.increment", "parallel", "RESTRICTED"),
					resource.TestCheckResourceAttr(
						"postgresql_function.increment", "security_definer", "true"),
					resource.TestCheckResourceAttr(
						"postgresql_function.increment", "volatility", "STABLE"),
				),
			},
		},
	})
}

func TestAccPostgresqlFunction_Update(t *testing.T) {
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

func testAccCheckPostgresqlFunctionExists(n string, database string) resource.TestCheckFunc {
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

func testAccCheckPostgresqlFunctionDestroy(s *terraform.State) error {
	client := testAccProvider.Meta().(*Client)

	for _, rs := range s.RootModule().Resources {
		if rs.Type != "postgresql_function" {
			continue
		}

		txn, err := startTransaction(client, "")
		if err != nil {
			return err
		}
		defer deferredRollback(txn)

		_, functionSignature, expandErr := expandFunctionID(rs.Primary.ID, nil, nil)

		if expandErr != nil {
			return fmt.Errorf("Incorrect resource Id %s", err)
		}

		exists, err := checkFunctionExists(txn, functionSignature)

		if err != nil {
			return fmt.Errorf("Error checking function %s", err)
		}

		if exists {
			return fmt.Errorf("Function still exists after destroy")
		}
	}

	return nil
}

func checkFunctionExists(txn *sql.Tx, signature string) (bool, error) {
	var _rez bool
	err := txn.QueryRow(fmt.Sprintf("SELECT to_regprocedure('%s') IS NOT NULL", signature)).Scan(&_rez)
	switch {
	case err == sql.ErrNoRows:
		return false, nil
	case err != nil:
		return false, fmt.Errorf("Error reading info about function: %s", err)
	}

	return _rez, nil
}
