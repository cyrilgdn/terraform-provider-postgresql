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
	//In addition, the db includes 4 system schemas: 'information_schema', 'pg_catalog', 'pg_toast' and 'public'
	//along with a variable number of 'pg_temp_*' and 'pg_toast_temp_*' temporary system schemas.
	//'public' is always included in the output regardless of the 'include_system_schemas' setting.
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
					resource.TestCheckResourceAttr("data.postgresql_schemas.system_false", "schemas.#", "8"),
					resource.TestCheckResourceAttr("data.postgresql_schemas.no_match", "schemas.#", "0"),
					resource.TestCheckResourceAttr("data.postgresql_schemas.system_false_like_exp", "schemas.#", "2"),
					resource.TestCheckResourceAttr("data.postgresql_schemas.system_false_like_exp", "schemas.0", "test_exp"),
					resource.TestCheckResourceAttr("data.postgresql_schemas.system_false_like_exp", "schemas.1", "exp_test"),
					resource.TestCheckResourceAttr("data.postgresql_schemas.system_true_like_exp", "schemas.#", "2"),
					resource.TestCheckResourceAttr("data.postgresql_schemas.like_pg", "schemas.#", "0"),
					resource.TestCheckResourceAttr("data.postgresql_schemas.system_false_like_pg", "schemas.#", "0"),
					resource.TestCheckResourceAttr("data.postgresql_schemas.system_false_like_pg_double_wildcard", "schemas.#", "1"),
					resource.TestCheckResourceAttr("data.postgresql_schemas.system_false_like_pg_double_wildcard", "schemas.0", "test_pg"),
					resource.TestCheckResourceAttr("data.postgresql_schemas.system_true_like_information_schema", "schemas.#", "1"),
					resource.TestCheckResourceAttr("data.postgresql_schemas.system_true_like_information_schema", "schemas.0", "information_schema"),
					resource.TestCheckResourceAttr("data.postgresql_schemas.like_test_schema", "schemas.#", "3"),
					resource.TestCheckResourceAttr("data.postgresql_schemas.like_test_schema", "schemas.0", "test_schema"),
					resource.TestCheckResourceAttr("data.postgresql_schemas.like_test_schema", "schemas.1", "test_schema1"),
					resource.TestCheckResourceAttr("data.postgresql_schemas.like_test_schema", "schemas.2", "test_schema2"),
					resource.TestCheckResourceAttr("data.postgresql_schemas.regex_test_schema", "schemas.#", "3"),
					resource.TestCheckResourceAttr("data.postgresql_schemas.regex_test_schema", "schemas.0", "test_schema"),
					resource.TestCheckResourceAttr("data.postgresql_schemas.regex_test_schema", "schemas.1", "test_schema1"),
					resource.TestCheckResourceAttr("data.postgresql_schemas.regex_test_schema", "schemas.2", "test_schema2"),
					resource.TestCheckResourceAttr("data.postgresql_schemas.system_true_not_like_pg", "schemas.#", "9"),
					resource.TestCheckResourceAttr("data.postgresql_schemas.system_true_like_pg_regex_pg_catalog", "schemas.#", "1"),
					resource.TestCheckResourceAttr("data.postgresql_schemas.system_true_like_pg_regex_pg_catalog", "schemas.0", "pg_catalog"),
					resource.TestCheckResourceAttr("data.postgresql_schemas.system_false_like_test_not_like_test_schema_regex_test_schema", "schemas.#", "2"),
					resource.TestCheckResourceAttr("data.postgresql_schemas.system_false_like_test_not_like_test_schema_regex_test_schema", "schemas.0", "test_schema"),
					resource.TestCheckResourceAttr("data.postgresql_schemas.system_false_like_test_not_like_test_schema_regex_test_schema", "schemas.1", "test_schema2"),
					resource.TestCheckResourceAttr("data.postgresql_schemas.system_false_likeany_multi", "schemas.#", "2"),
					resource.TestCheckResourceAttr("data.postgresql_schemas.system_true_not_like_multi", "schemas.#", "6"),
					resource.TestCheckResourceAttr("data.postgresql_schemas.system_true_likeall_multi_not_like_multi", "schemas.#", "1"),
					resource.TestCheckResourceAttr("data.postgresql_schemas.system_true_likeall_multi_not_like_multi", "schemas.0", "test_schema1"),
					resource.TestCheckResourceAttr("data.postgresql_schemas.system_true_likeany_multi_not_like_multi", "schemas.#", "3"),
					resource.TestCheckResourceAttr("data.postgresql_schemas.system_true_likeall_multi_not_like_multi_regex", "schemas.#", "1"),
					resource.TestCheckResourceAttr("data.postgresql_schemas.system_true_likeall_multi_not_like_multi_regex", "schemas.0", "test_exp"),
					resource.TestCheckResourceAttr("data.postgresql_schemas.system_true_likeany_multi_not_like_multi_regex", "schemas.#", "3"),
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

	data "postgresql_schemas" "no_match" {
		database = "%[1]s"
		like_any_patterns = ["no_match"]
	}

	data "postgresql_schemas" "system_false_like_exp" {
		database = "%[1]s"
		include_system_schemas = false
		like_any_patterns = ["%%exp%%"]
	}

	data "postgresql_schemas" "system_true_like_exp" {
		database = "%[1]s"
		include_system_schemas = true
		like_any_patterns = ["%%exp%%"]
	}

	data "postgresql_schemas" "like_pg" {
		database = "%[1]s"
		like_any_patterns = ["pg_%%"]
	}

	data "postgresql_schemas" "system_false_like_pg" {
		database = "%[1]s"
		include_system_schemas = false
		like_any_patterns = ["pg_%%"]
	}

	data "postgresql_schemas" "system_false_like_pg_double_wildcard" {
		database = "%[1]s"
		include_system_schemas = false
		like_all_patterns = ["%%pg%%"]
	}

	data "postgresql_schemas" "system_true_like_information_schema" {
		database = "%[1]s"
		include_system_schemas = true
		like_all_patterns = ["information_schema%%"]
	}

	data "postgresql_schemas" "like_test_schema" {
		database = "%[1]s"
		like_all_patterns = ["test_schema%%"]
	}

	data "postgresql_schemas" "regex_test_schema" {
		database = "%[1]s"
		regex_pattern = "^test_schema.*$"
	}

	data "postgresql_schemas" "system_true_not_like_pg" {
		database = "%[1]s"
		include_system_schemas = true
		not_like_all_patterns = ["pg_%%"]
	}

	data "postgresql_schemas" "system_true_like_pg_regex_pg_catalog" {
		database = "%[1]s"
		include_system_schemas = true
		like_any_patterns = ["pg_%%"]
		regex_pattern = "^pg_catalog.*$"
	}

	data "postgresql_schemas" "system_false_like_test_not_like_test_schema_regex_test_schema" {
		database = "%[1]s"
		include_system_schemas = false
		like_any_patterns = ["test_%%"]
		not_like_all_patterns = ["test_schema1%%"]
		regex_pattern = "^test_schema.*$"
	}

	data "postgresql_schemas" "system_false_likeany_multi" {
		database = "%[1]s"
		include_system_schemas = false
		like_any_patterns = ["test_schema1","test_exp"]
	}

	data "postgresql_schemas" "system_true_not_like_multi" {
		database = "%[1]s"
		include_system_schemas = true
		not_like_all_patterns = ["%%pg%%","%%exp%%"]
	}	

	data "postgresql_schemas" "system_true_likeall_multi_not_like_multi" {
		database = "%[1]s"
		include_system_schemas = true
		like_all_patterns = ["%%test%%", "%%1"]
		not_like_all_patterns = ["%%pg%%","%%exp%%"]
	}

	data "postgresql_schemas" "system_true_likeany_multi_not_like_multi" {
		database = "%[1]s"
		include_system_schemas = true
		like_any_patterns = ["%%test%%", "%%1"]
		not_like_all_patterns = ["%%pg%%","%%exp%%"]
	}

	data "postgresql_schemas" "system_true_likeall_multi_not_like_multi_regex" {
		database = "%[1]s"
		include_system_schemas = true
		like_all_patterns= ["%%exp%%", "%%test%%"]
		not_like_all_patterns = ["%%1%%","%%2%%"]
		regex_pattern = "^test_.*$"
	}

	data "postgresql_schemas" "system_true_likeany_multi_not_like_multi_regex" {
		database = "%[1]s"
		include_system_schemas = true
		like_any_patterns= ["%%exp%%", "%%test%%"]
		not_like_all_patterns = ["%%1%%","%%2%%"]
		regex_pattern = "^test_.*$"
	}
	`, dbName)
}
