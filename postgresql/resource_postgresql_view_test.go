package postgresql

import (
	"database/sql"
	"fmt"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
)

func TestAccPostgresqlView_Basic(t *testing.T) {
	config := `
resource "postgresql_view" "basic_view" {
    name = "basic_view"
    query = <<-EOF
		SELECT *
		FROM unnest(ARRAY[1]) AS element;
	EOF
}
`

	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccPreCheck(t)
			testCheckCompatibleVersion(t, featureView)
		},
		Providers:    testAccProviders,
		CheckDestroy: testAccCheckPostgresqlViewDestroy,
		Steps: []resource.TestStep{
			{
				Config: config,
				Check: resource.ComposeTestCheckFunc(
					testAccCheckPostgresqlViewExists("postgresql_view.basic_view", ""),
					resource.TestCheckResourceAttr(
						"postgresql_view.basic_view", "schema", "public"),
					resource.TestCheckResourceAttr(
						"postgresql_view.basic_view", "name", "basic_view"),
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

func TestAccPostgresqlView_CaseSensitiveViewName(t *testing.T) {
	config := `
resource "postgresql_view" "case_sensitive_view_name" {
	name = "Case_Sensitive_View_Name"
	query = <<-EOF
		SELECT 1 AS one;
	EOF
}
`

	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccPreCheck(t)
			testCheckCompatibleVersion(t, featureView)
		},
		Providers:    testAccProviders,
		CheckDestroy: testAccCheckPostgresqlViewDestroy,
		Steps: []resource.TestStep{
			{
				Config: config,
				Check: resource.ComposeTestCheckFunc(
					testAccCheckPostgresqlViewExists("postgresql_view.case_sensitive_view_name", ""),
					resource.TestCheckResourceAttr(
						"postgresql_view.case_sensitive_view_name", "schema", "public"),
					resource.TestCheckResourceAttr(
						"postgresql_view.case_sensitive_view_name", "name", "Case_Sensitive_View_Name"),
					resource.TestCheckResourceAttr(
						"postgresql_view.case_sensitive_view_name", "with_check_option", ""),
					resource.TestCheckResourceAttr(
						"postgresql_view.case_sensitive_view_name", "with_security_barrier", "false"),
					resource.TestCheckResourceAttr(
						"postgresql_view.case_sensitive_view_name", "with_security_invoker", "false"),
					resource.TestCheckResourceAttr(
						"postgresql_view.case_sensitive_view_name", "drop_cascade", "false"),
				),
			},
		},
	})
}

func TestAccPostgresqlView_QueryWithDoubleQuotes(t *testing.T) {
	config := `
resource "postgresql_view" "double_quotes_query_view" {
    name = "double_quotes_query_view"
    query = <<-EOF
SELECT 1 AS "One", 2 AS two;
	EOF
}
`

	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccPreCheck(t)
			testCheckCompatibleVersion(t, featureView)
		},
		Providers:    testAccProviders,
		CheckDestroy: testAccCheckPostgresqlViewDestroy,
		Steps: []resource.TestStep{
			{
				Config: config,
				Check: resource.ComposeTestCheckFunc(
					testAccCheckPostgresqlViewExists("postgresql_view.double_quotes_query_view", ""),
					resource.TestCheckResourceAttr(
						"postgresql_view.double_quotes_query_view", "schema", "public"),
					resource.TestCheckResourceAttr(
						"postgresql_view.double_quotes_query_view", "name", "double_quotes_query_view"),
					resource.TestCheckResourceAttr(
						"postgresql_view.double_quotes_query_view", "query", "SELECT 1 AS \"One\", 2 AS two;\n"),
					resource.TestCheckResourceAttr(
						"postgresql_view.double_quotes_query_view", "with_check_option", ""),
					resource.TestCheckResourceAttr(
						"postgresql_view.double_quotes_query_view", "with_security_barrier", "false"),
					resource.TestCheckResourceAttr(
						"postgresql_view.double_quotes_query_view", "with_security_invoker", "false"),
					resource.TestCheckResourceAttr(
						"postgresql_view.double_quotes_query_view", "drop_cascade", "false"),
				),
			},
		},
	})
}

func TestAccPostgresqlView_SpecificDatabase(t *testing.T) {
	skipIfNotAcc(t)

	dbSuffix, teardown := setupTestDatabase(t, true, true)
	defer teardown()

	dbName, _ := getTestDBNames(dbSuffix)

	config := `
resource "postgresql_view" "basic_view" {
	database = "%s"
	schema = "test_schema"
	name = "basic_view"
	query = <<-EOF
		SELECT *
		FROM unnest(ARRAY[1]) AS element;
	EOF
}
`
	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccPreCheck(t)
			testCheckCompatibleVersion(t, featureView)
		},
		Providers:    testAccProviders,
		CheckDestroy: testAccCheckPostgresqlViewDestroy,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(config, dbName),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckPostgresqlViewExists("postgresql_view.basic_view", dbName),
					resource.TestCheckResourceAttr(
						"postgresql_view.basic_view", "database", dbName),
					resource.TestCheckResourceAttr(
						"postgresql_view.basic_view", "schema", "test_schema"),
					resource.TestCheckResourceAttr(
						"postgresql_view.basic_view", "name", "basic_view"),
					resource.TestCheckResourceAttr(
						"postgresql_view.basic_view", "with_check_option", ""),
				),
			},
		},
	})
}

func TestAccPostgresqlView_AllOptions(t *testing.T) {
	skipIfNotAcc(t)

	dbSuffix, teardown := setupTestDatabase(t, true, true)
	defer teardown()

	dbName, _ := getTestDBNames(dbSuffix)

	config := `
resource "postgresql_view" "all_option_view" {
	database = "%s"
	schema = "test_schema"
	name = "all_option_view"
	query = <<-EOF
		SELECT schemaname, tablename
		FROM pg_catalog.pg_tables;
	EOF
	with_check_option = "CASCADED"
	with_security_barrier = true
	with_security_invoker = true
	drop_cascade = true
}
`
	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccPreCheck(t)
			testCheckCompatibleVersion(t, featureView)
		},
		Providers:    testAccProviders,
		CheckDestroy: testAccCheckPostgresqlViewDestroy,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(config, dbName),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckPostgresqlViewExists("postgresql_view.all_option_view", dbName),
					resource.TestCheckResourceAttr(
						"postgresql_view.all_option_view", "database", dbName),
					resource.TestCheckResourceAttr(
						"postgresql_view.all_option_view", "schema", "test_schema"),
					resource.TestCheckResourceAttr(
						"postgresql_view.all_option_view", "name", "all_option_view"),
					resource.TestCheckResourceAttr(
						"postgresql_view.all_option_view", "with_check_option", "CASCADED"),
					resource.TestCheckResourceAttr(
						"postgresql_view.all_option_view", "with_security_barrier", "true"),
					resource.TestCheckResourceAttr(
						"postgresql_view.all_option_view", "with_security_invoker", "true"),
					resource.TestCheckResourceAttr(
						"postgresql_view.all_option_view", "drop_cascade", "true"),
				),
			},
		},
	})
}

func TestAccPostgresqlView_Update(t *testing.T) {
	configCreate := `
resource "postgresql_view" "pg_view" {
    name = "pg_view"
	query = <<-EOF
		SELECT schemaname, tablename
		FROM pg_catalog.pg_tables;
	EOF
}
`

	configUpdate := `
resource "postgresql_view" "pg_view" {
	name = "pg_view"
	query = <<-EOF
		SELECT schemaname, tablename, tableowner
		FROM pg_catalog.pg_tables;
	EOF
	with_check_option = "CASCADED"
	with_security_barrier = true
	with_security_invoker = true
	drop_cascade = true
}
`
	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccPreCheck(t)
			testCheckCompatibleVersion(t, featureView)
		},
		Providers:    testAccProviders,
		CheckDestroy: testAccCheckPostgresqlViewDestroy,
		Steps: []resource.TestStep{
			{
				Config: configCreate,
				Check: resource.ComposeTestCheckFunc(
					testAccCheckPostgresqlViewExists("postgresql_view.pg_view", ""),
					resource.TestCheckResourceAttr(
						"postgresql_view.pg_view", "schema", "public"),
					resource.TestCheckResourceAttr(
						"postgresql_view.pg_view", "name", "pg_view"),
					resource.TestCheckResourceAttr(
						"postgresql_view.pg_view", "with_check_option", ""),
					resource.TestCheckResourceAttr(
						"postgresql_view.pg_view", "with_security_barrier", "false"),
					resource.TestCheckResourceAttr(
						"postgresql_view.pg_view", "with_security_invoker", "false"),
					resource.TestCheckResourceAttr(
						"postgresql_view.pg_view", "drop_cascade", "false"),
				),
			},
			{
				Config: configUpdate,
				Check: resource.ComposeTestCheckFunc(
					testAccCheckPostgresqlViewExists("postgresql_view.pg_view", ""),
					resource.TestCheckResourceAttr(
						"postgresql_view.pg_view", "schema", "public"),
					resource.TestCheckResourceAttr(
						"postgresql_view.pg_view", "name", "pg_view"),
					resource.TestCheckResourceAttr(
						"postgresql_view.pg_view", "with_check_option", "CASCADED"),
					resource.TestCheckResourceAttr(
						"postgresql_view.pg_view", "with_security_barrier", "true"),
					resource.TestCheckResourceAttr(
						"postgresql_view.pg_view", "with_security_invoker", "true"),
					resource.TestCheckResourceAttr(
						"postgresql_view.pg_view", "drop_cascade", "true"),
				),
			},
		},
	})
}

func TestAccPostgresqlView_QuerySelectColumnChange(t *testing.T) {
	configCreate := `
resource "postgresql_view" "pg_view" {
    name = "pg_view"
	query = "SELECT 1 as one;"
}
`

	configUpdateAddColumn := `
resource "postgresql_view" "pg_view" {
    name = "pg_view"
	query = "SELECT 1 as one, 2 as two;"
}`

	configUpdateReplaceColumn := `
resource "postgresql_view" "pg_view" {
    name = "pg_view"
	query = "SELECT 1 as one, 3 as three;"
}`

	configUpdateDeleteColumn := `
resource "postgresql_view" "pg_view" {
    name = "pg_view"
	query = "SELECT 1 as one;"
}`

	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccPreCheck(t)
			testCheckCompatibleVersion(t, featureView)
		},
		Providers:    testAccProviders,
		CheckDestroy: testAccCheckPostgresqlViewDestroy,
		Steps: []resource.TestStep{
			{
				Config: configCreate,
				Check: resource.ComposeTestCheckFunc(
					testAccCheckPostgresqlViewExists("postgresql_view.pg_view", ""),
					resource.TestCheckResourceAttr(
						"postgresql_view.pg_view", "schema", "public"),
					resource.TestCheckResourceAttr(
						"postgresql_view.pg_view", "name", "pg_view"),
					resource.TestCheckResourceAttr(
						"postgresql_view.pg_view", "query", "SELECT 1 as one;"),
					resource.TestCheckResourceAttr(
						"postgresql_view.pg_view", "with_check_option", ""),
					resource.TestCheckResourceAttr(
						"postgresql_view.pg_view", "with_security_barrier", "false"),
					resource.TestCheckResourceAttr(
						"postgresql_view.pg_view", "with_security_invoker", "false"),
					resource.TestCheckResourceAttr(
						"postgresql_view.pg_view", "drop_cascade", "false"),
				),
			},
			{
				Config: configUpdateAddColumn,
				Check: resource.ComposeTestCheckFunc(
					testAccCheckPostgresqlViewExists("postgresql_view.pg_view", ""),
					resource.TestCheckResourceAttr(
						"postgresql_view.pg_view", "schema", "public"),
					resource.TestCheckResourceAttr(
						"postgresql_view.pg_view", "name", "pg_view"),
					resource.TestCheckResourceAttr(
						"postgresql_view.pg_view", "query", "SELECT 1 as one, 2 as two;"),
					resource.TestCheckResourceAttr(
						"postgresql_view.pg_view", "with_check_option", ""),
					resource.TestCheckResourceAttr(
						"postgresql_view.pg_view", "with_security_barrier", "false"),
					resource.TestCheckResourceAttr(
						"postgresql_view.pg_view", "with_security_invoker", "false"),
					resource.TestCheckResourceAttr(
						"postgresql_view.pg_view", "drop_cascade", "false"),
				),
			},
			{
				Config: configUpdateReplaceColumn,
				Check: resource.ComposeTestCheckFunc(
					testAccCheckPostgresqlViewExists("postgresql_view.pg_view", ""),
					resource.TestCheckResourceAttr(
						"postgresql_view.pg_view", "schema", "public"),
					resource.TestCheckResourceAttr(
						"postgresql_view.pg_view", "name", "pg_view"),
					resource.TestCheckResourceAttr(
						"postgresql_view.pg_view", "query", "SELECT 1 as one, 3 as three;"),
					resource.TestCheckResourceAttr(
						"postgresql_view.pg_view", "with_check_option", ""),
					resource.TestCheckResourceAttr(
						"postgresql_view.pg_view", "with_security_barrier", "false"),
					resource.TestCheckResourceAttr(
						"postgresql_view.pg_view", "with_security_invoker", "false"),
					resource.TestCheckResourceAttr(
						"postgresql_view.pg_view", "drop_cascade", "false"),
				),
			},
			{
				Config: configUpdateDeleteColumn,
				Check: resource.ComposeTestCheckFunc(
					testAccCheckPostgresqlViewExists("postgresql_view.pg_view", ""),
					resource.TestCheckResourceAttr(
						"postgresql_view.pg_view", "schema", "public"),
					resource.TestCheckResourceAttr(
						"postgresql_view.pg_view", "name", "pg_view"),
					resource.TestCheckResourceAttr(
						"postgresql_view.pg_view", "query", "SELECT 1 as one;"),
					resource.TestCheckResourceAttr(
						"postgresql_view.pg_view", "with_check_option", ""),
					resource.TestCheckResourceAttr(
						"postgresql_view.pg_view", "with_security_barrier", "false"),
					resource.TestCheckResourceAttr(
						"postgresql_view.pg_view", "with_security_invoker", "false"),
					resource.TestCheckResourceAttr(
						"postgresql_view.pg_view", "drop_cascade", "false"),
				),
			},
		},
	})
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

		exists, err := checkViewExists(txn, signature)

		if err != nil {
			return fmt.Errorf("Error checking function %s", err)
		}

		if !exists {
			return fmt.Errorf("View not found")
		}

		return nil
	}
}

func testAccCheckPostgresqlViewDestroy(s *terraform.State) error {
	client := testAccProvider.Meta().(*Client)

	for _, rs := range s.RootModule().Resources {
		if rs.Type != "postgresql_view" {
			continue
		}

		viewIdentifier := rs.Primary.ID

		txn, err := startTransaction(client, "")
		if err != nil {
			return err
		}
		defer deferredRollback(txn)

		exists, err := checkViewExists(txn, viewIdentifier)

		if err != nil {
			return fmt.Errorf("Error checking view %s", err)
		}

		if exists {
			return fmt.Errorf("View still exists after destroy")
		}
	}

	return nil
}

func checkViewExists(txn *sql.Tx, signature string) (bool, error) {
	var exists bool
	signatureParts := strings.Split(signature, ".")
	schema := signatureParts[1]
	viewName := signatureParts[2]
	err := txn.QueryRow(fmt.Sprintf("SELECT viewname IS NOT NULL FROM pg_views where schemaname = '%s' AND viewname = '%s'", schema, viewName)).Scan(&exists)
	switch {
	case err == sql.ErrNoRows:
		return false, nil
	case err != nil:
		return false, fmt.Errorf("Error reading info about view: %s", err)
	}

	return exists, nil
}
