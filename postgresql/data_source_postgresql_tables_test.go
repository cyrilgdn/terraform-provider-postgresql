package postgresql

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
)

func TestAccPostgresqlDataSourceTables(t *testing.T) {
	skipIfNotAcc(t)

	// Create the database outside of resource.Test
	// because we need to create test schemas.
	dbSuffix, teardown := setupTestDatabase(t, true, true)
	defer teardown()

	schemas := []string{"test_schema1", "test_schema2"}
	createTestSchemas(t, dbSuffix, schemas, "")

	testTables := []string{"test_schema.test_table", "test_schema1.test_table1", "test_schema1.test_table2", "test_schema2.test_table1"}
	createTestTables(t, dbSuffix, testTables, "")

	dbName, _ := getTestDBNames(dbSuffix)

	testAccPostgresqlDataSourceTablesDatabaseConfig := generateDataSourceTablesConfig(dbName)

	resource.Test(t, resource.TestCase{
		PreCheck:  func() { testAccPreCheck(t) },
		Providers: testAccProviders,
		Steps: []resource.TestStep{
			{
				Config: testAccPostgresqlDataSourceTablesDatabaseConfig,
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("data.postgresql_tables.test_schema", "tables.#", "1"),
					resource.TestCheckResourceAttr("data.postgresql_tables.test_schema", "tables.0.object_name", "test_table"),
					resource.TestCheckResourceAttr("data.postgresql_tables.test_schema", "tables.0.schema_name", "test_schema"),
					resource.TestCheckResourceAttr("data.postgresql_tables.test_schema", "tables.0.table_type", "BASE TABLE"),
					resource.TestCheckResourceAttr("data.postgresql_tables.test_schemas1and2", "tables.#", "3"),
					resource.TestCheckResourceAttr("data.postgresql_tables.test_schemas1and2_type_base", "tables.#", "3"),
					resource.TestCheckResourceAttr("data.postgresql_tables.test_schemas1and2_type_other", "tables.#", "0"),
					resource.TestCheckResourceAttr("data.postgresql_tables.test_schemas1and2_type_base_and_other", "tables.#", "3"),
					resource.TestCheckResourceAttr("data.postgresql_tables.test_schemas_like_all_table1", "tables.#", "2"),
					resource.TestCheckResourceAttr("data.postgresql_tables.test_schemas_like_all_table1and2", "tables.#", "0"),
					resource.TestCheckResourceAttr("data.postgresql_tables.test_schemas_like_any_table1and2", "tables.#", "3"),
					resource.TestCheckResourceAttr("data.postgresql_tables.test_schemas_not_like_all_table1and2", "tables.#", "1"),
					resource.TestCheckResourceAttr("data.postgresql_tables.test_schemas_not_like_all_table1and2", "tables.0.object_name", "test_table"),
					resource.TestCheckResourceAttr("data.postgresql_tables.test_schemas_regex_table1", "tables.#", "2"),
					resource.TestCheckResourceAttr("data.postgresql_tables.test_schemas_combine_filtering", "tables.#", "1"),
					resource.TestCheckResourceAttr("data.postgresql_tables.test_schemas_combine_filtering", "tables.0.object_name", "test_table2"),
					resource.TestCheckResourceAttr("data.postgresql_tables.test_schemas_combine_filtering", "tables.0.schema_name", "test_schema1"),
				),
			},
		},
	})
}

func generateDataSourceTablesConfig(dbName string) string {
	return fmt.Sprintf(`	
	data "postgresql_tables" "test_schemas1and2" {
		database = "%[1]s"
		schemas = ["test_schema1","test_schema2"]
	}

	data "postgresql_tables" "test_schema" {
		database = "%[1]s"
		schemas = ["test_schema"]
	}

	data "postgresql_tables" "test_schemas1and2_type_base" {
		database = "%[1]s"
		schemas = ["test_schema1","test_schema2"]
		table_types = ["BASE TABLE"]
	}

	data "postgresql_tables" "test_schemas1and2_type_other" {
		database = "%[1]s"
		schemas = ["test_schema1","test_schema2"]
		table_types = ["VIEW","LOCAL TEMPORARY"]
	}

	data "postgresql_tables" "test_schemas1and2_type_base_and_other" {
		database = "%[1]s"
		schemas = ["test_schema1","test_schema2"]
		table_types = ["VIEW","LOCAL TEMPORARY","BASE TABLE"]
	}

	data "postgresql_tables" "test_schemas_like_all_table1" {
		database = "%[1]s"
		schemas = ["test_schema","test_schema1","test_schema2"]
		like_all_patterns = ["test_table1"]
	}

	data "postgresql_tables" "test_schemas_like_all_table1and2" {
		database = "%[1]s"
		schemas = ["test_schema","test_schema1","test_schema2"]
		like_all_patterns = ["test_table1","test_table2"]
	}

	data "postgresql_tables" "test_schemas_like_any_table1and2" {
		database = "%[1]s"
		schemas = ["test_schema","test_schema1","test_schema2"]
		like_any_patterns = ["test_table1","test_table2"]
	}

	data "postgresql_tables" "test_schemas_not_like_all_table1and2" {
		database = "%[1]s"
		schemas = ["test_schema","test_schema1","test_schema2"]
		not_like_all_patterns = ["test_table1","test_table2"]
	}

	data "postgresql_tables" "test_schemas_regex_table1" {
		database = "%[1]s"
		schemas = ["test_schema","test_schema1","test_schema2"]
		regex_pattern = "^test_table1$"
	}

	data "postgresql_tables" "test_schemas_combine_filtering" {
		database = "%[1]s"
		schemas = ["test_schema","test_schema1","test_schema2"]
		like_any_patterns= ["%%2%%"]
		not_like_all_patterns = ["%%1%%"]
		regex_pattern = "^test_.*$"
	}

	# test_basic's output won't be checked as it can return an indeterminate number of system tables
	data "postgresql_tables" "test_basic" {
		database = "%[1]s"
	}

	`, dbName)
}
