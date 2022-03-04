package postgresql

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
)

func TestAccPostgresqlDataSourceSequences(t *testing.T) {
	skipIfNotAcc(t)

	// Create the database outside of resource.Test
	// because we need to create test schemas.
	dbSuffix, teardown := setupTestDatabase(t, true, true)
	defer teardown()

	schemas := []string{"test_schema1", "test_schema2"}
	createTestSchemas(t, dbSuffix, schemas, "")

	testSequences := []string{"test_schema.test_sequence", "test_schema1.test_sequence1", "test_schema1.test_sequence2", "test_schema2.test_sequence1"}
	createTestSequences(t, dbSuffix, testSequences, "")

	dbName, _ := getTestDBNames(dbSuffix)

	testAccPostgresqlDataSourceSequencesDatabaseConfig := generateDataSourceSequencesConfig(dbName)

	resource.Test(t, resource.TestCase{
		PreCheck:  func() { testAccPreCheck(t) },
		Providers: testAccProviders,
		Steps: []resource.TestStep{
			{
				Config: testAccPostgresqlDataSourceSequencesDatabaseConfig,
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("data.postgresql_sequences.test_schema", "sequences.#", "1"),
					resource.TestCheckResourceAttr("data.postgresql_sequences.test_schema", "sequences.0.object_name", "test_sequence"),
					resource.TestCheckResourceAttr("data.postgresql_sequences.test_schema", "sequences.0.schema_name", "test_schema"),
					resource.TestCheckResourceAttr("data.postgresql_sequences.test_schemas1and2", "sequences.#", "3"),
					resource.TestCheckResourceAttr("data.postgresql_sequences.test_schemas_like_all_sequence1", "sequences.#", "2"),
					resource.TestCheckResourceAttr("data.postgresql_sequences.test_schemas_like_all_sequence1and2", "sequences.#", "0"),
					resource.TestCheckResourceAttr("data.postgresql_sequences.test_schemas_like_any_sequence1and2", "sequences.#", "3"),
					resource.TestCheckResourceAttr("data.postgresql_sequences.test_schemas_not_like_all_sequence1and2", "sequences.#", "1"),
					resource.TestCheckResourceAttr("data.postgresql_sequences.test_schemas_not_like_all_sequence1and2", "sequences.0.object_name", "test_sequence"),
					resource.TestCheckResourceAttr("data.postgresql_sequences.test_schemas_regex_sequence1", "sequences.#", "2"),
					resource.TestCheckResourceAttr("data.postgresql_sequences.test_schemas_combine_filtering", "sequences.#", "1"),
					resource.TestCheckResourceAttr("data.postgresql_sequences.test_schemas_combine_filtering", "sequences.0.object_name", "test_sequence2"),
					resource.TestCheckResourceAttr("data.postgresql_sequences.test_schemas_combine_filtering", "sequences.0.schema_name", "test_schema1"),
				),
			},
		},
	})
}

func generateDataSourceSequencesConfig(dbName string) string {
	return fmt.Sprintf(`	
	data "postgresql_sequences" "test_schemas1and2" {
		database = "%[1]s"
		schemas = ["test_schema1","test_schema2"]
	}

	data "postgresql_sequences" "test_schema" {
		database = "%[1]s"
		schemas = ["test_schema"]
	}

	data "postgresql_sequences" "test_schemas_like_all_sequence1" {
		database = "%[1]s"
		schemas = ["test_schema","test_schema1","test_schema2"]
		like_all_patterns = ["test_sequence1"]
	}

	data "postgresql_sequences" "test_schemas_like_all_sequence1and2" {
		database = "%[1]s"
		schemas = ["test_schema","test_schema1","test_schema2"]
		like_all_patterns = ["test_sequence1","test_sequence2"]
	}

	data "postgresql_sequences" "test_schemas_like_any_sequence1and2" {
		database = "%[1]s"
		schemas = ["test_schema","test_schema1","test_schema2"]
		like_any_patterns = ["test_sequence1","test_sequence2"]
	}

	data "postgresql_sequences" "test_schemas_not_like_all_sequence1and2" {
		database = "%[1]s"
		schemas = ["test_schema","test_schema1","test_schema2"]
		not_like_all_patterns = ["test_sequence1","test_sequence2"]
	}

	data "postgresql_sequences" "test_schemas_regex_sequence1" {
		database = "%[1]s"
		schemas = ["test_schema","test_schema1","test_schema2"]
		regex_pattern = "^test_sequence1$"
	}

	data "postgresql_sequences" "test_schemas_combine_filtering" {
		database = "%[1]s"
		schemas = ["test_schema","test_schema1","test_schema2"]
		like_any_patterns= ["%%2%%"]
		not_like_all_patterns = ["%%1%%"]
		regex_pattern = "^test_.*$"
	}

	# test_basic's output won't be checked as it can return an indeterminate number of system sequences
	data "postgresql_sequences" "test_basic" {
		database = "%[1]s"
	}

	`, dbName)
}
