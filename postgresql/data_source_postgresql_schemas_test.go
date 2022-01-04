package postgresql

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
)

func TestAccPostgresqlDataSourceSchemas(t *testing.T) {
	skipIfNotAcc(t)

	// Create the database outside of resource.Test
	// because we need to create test schemas.
	dbSuffix, teardown := setupTestDatabase(t, true, true)
	defer teardown()

	//Note that the db will also include 'test_schema' and 'dev_schema' from setupTestDatabase along with these schemas.
	//In addition, the db includes 4 reserved schemas: "information_schema", "pg_catalog", "pg_toast" and "public".
	schemas := []string{"test_schema1", "test_schema2", "test_exp", "exp_test", "test_pg"}
	createTestSchemas(t, dbSuffix, schemas, "")

	dbName, _ := getTestDBNames(dbSuffix)

	testAccPostgresqlDataSourceSchemasDatabaseConfig := generateDataSourceSchemasConfig(dbName)

	resource.Test(t, resource.TestCase{
		PreCheck:  func() { testAccPreCheck(t) },
		Providers: testAccProviders,
		Steps: []resource.TestStep{
			{
				Config: testAccPostgresqlDataSourceSchemasDatabaseConfig,
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("data.postgresql_schemas.system_false", "schemas.#", "7"),
					resource.TestCheckResourceAttr("data.postgresql_schemas.system_true", "schemas.#", "11"),
					resource.TestCheckResourceAttr("data.postgresql_schemas.no_match", "schemas.#", "0"),
					resource.TestCheckResourceAttr("data.postgresql_schemas.system_false_like_exp", "schemas.#", "2"),
					resource.TestCheckResourceAttr("data.postgresql_schemas.system_false_like_exp", "schemas.0", "test_exp"),
					resource.TestCheckResourceAttr("data.postgresql_schemas.system_false_like_exp", "schemas.1", "exp_test"),
					resource.TestCheckResourceAttr("data.postgresql_schemas.system_true_like_exp", "schemas.#", "2"),
					resource.TestCheckResourceAttr("data.postgresql_schemas.like_pg", "schemas.#", "0"),
					resource.TestCheckResourceAttr("data.postgresql_schemas.system_true_like_pg", "schemas.#", "2"),
					resource.TestCheckResourceAttr("data.postgresql_schemas.like_test_schema", "schemas.#", "3"),
					resource.TestCheckResourceAttr("data.postgresql_schemas.like_test_schema", "schemas.0", "test_schema"),
					resource.TestCheckResourceAttr("data.postgresql_schemas.like_test_schema", "schemas.1", "test_schema1"),
					resource.TestCheckResourceAttr("data.postgresql_schemas.like_test_schema", "schemas.2", "test_schema2"),
					resource.TestCheckResourceAttr("data.postgresql_schemas.regex_test_schema", "schemas.#", "3"),
					resource.TestCheckResourceAttr("data.postgresql_schemas.regex_test_schema", "schemas.0", "test_schema"),
					resource.TestCheckResourceAttr("data.postgresql_schemas.regex_test_schema", "schemas.1", "test_schema1"),
					resource.TestCheckResourceAttr("data.postgresql_schemas.regex_test_schema", "schemas.2", "test_schema2"),
					resource.TestCheckResourceAttr("data.postgresql_schemas.system_true_not_like_pg", "schemas.#", "9"),
					resource.TestCheckResourceAttr("data.postgresql_schemas.system_false_like_pg", "schemas.#", "0"),
					resource.TestCheckResourceAttr("data.postgresql_schemas.system_true_like_pg_regex_pg_toast", "schemas.#", "1"),
					resource.TestCheckResourceAttr("data.postgresql_schemas.system_true_like_pg_regex_pg_toast", "schemas.0", "pg_toast"),
					resource.TestCheckResourceAttr("data.postgresql_schemas.system_false_like_test_not_like_test_schema_regex_test_schema", "schemas.#", "2"),
					resource.TestCheckResourceAttr("data.postgresql_schemas.system_false_like_test_not_like_test_schema_regex_test_schema", "schemas.0", "test_schema"),
					resource.TestCheckResourceAttr("data.postgresql_schemas.system_false_like_test_not_like_test_schema_regex_test_schema", "schemas.1", "test_schema2"),
				),
			},
		},
	})
}

func generateDataSourceSchemasConfig(dbName string) string {
	return fmt.Sprintf(`	
	data "postgresql_schemas" "system_false" {
		database = "%[1]s"
		include_system_schemas = false
	}

	data "postgresql_schemas" "system_true" {
		database = "%[1]s"
		include_system_schemas = true
	}

	data "postgresql_schemas" "no_match" {
		database = "%[1]s"
		like_pattern = "no_match"
	}

	data "postgresql_schemas" "system_false_like_exp" {
		database = "%[1]s"
		include_system_schemas = false
		like_pattern = "%%exp%%"
	}

	data "postgresql_schemas" "system_true_like_exp" {
		database = "%[1]s"
		include_system_schemas = true
		like_pattern = "%%exp%%"
	}

	data "postgresql_schemas" "like_pg" {
		database = "%[1]s"
		like_pattern = "pg_%%"
	}

	data "postgresql_schemas" "system_true_like_pg" {
		database = "%[1]s"
		include_system_schemas = true
		like_pattern = "pg_%%"
	}

	data "postgresql_schemas" "like_test_schema" {
		database = "%[1]s"
		like_pattern = "test_schema%%"
	}

	data "postgresql_schemas" "regex_test_schema" {
		database = "%[1]s"
		regex_pattern = "^test_schema.*$"
	}

	data "postgresql_schemas" "system_true_not_like_pg" {
		database = "%[1]s"
		include_system_schemas = true
		not_like_pattern = "pg_%%"
	}

	data "postgresql_schemas" "system_false_like_pg" {
		database = "%[1]s"
		include_system_schemas = false
		like_pattern = "pg_%%"
	}

	data "postgresql_schemas" "system_true_like_pg_regex_pg_toast" {
		database = "%[1]s"
		include_system_schemas = true
		like_pattern = "pg_%%"
		regex_pattern = "^pg_toast.*$"
	}

	data "postgresql_schemas" "system_false_like_test_not_like_test_schema_regex_test_schema" {
		database = "%[1]s"
		include_system_schemas = false
		like_pattern = "test_%%"
		not_like_pattern = "test_schema1%%"
		regex_pattern = "^test_schema.*$"
	}
	`, dbName)
}
