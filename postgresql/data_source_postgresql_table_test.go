package postgresql

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
)

func TestAccPostgresqlDataSourceTable(t *testing.T) {
	skipIfNotAcc(t)

	// Create the database outside of resource.Test
	// because we need to create test schemas.
	dbSuffix, teardown := setupTestDatabase(t, true, true)
	defer teardown()

	testTable := "test_schema.test_table"
	createTestTables(t, dbSuffix, []string{testTable}, "")

	dbName, _ := getTestDBNames(dbSuffix)

	testAccPostgresqlDataSourceTablesDatabaseConfig := generateDataSourceTableConfig(dbName)

	resource.Test(t, resource.TestCase{
		PreCheck:  func() { testAccPreCheck(t) },
		Providers: testAccProviders,
		Steps: []resource.TestStep{
			{
				Config: testAccPostgresqlDataSourceTablesDatabaseConfig,
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("data.postgresql_table.test_table", "columns.#", "3"),
					resource.TestCheckResourceAttr("data.postgresql_table.test_table", "columns.0.name", "val"),
					resource.TestCheckResourceAttr("data.postgresql_table.test_table", "columns.0.type", "text"),
					resource.TestCheckResourceAttr("data.postgresql_table.test_table", "columns.0.is_primary_key", "false"),
					resource.TestCheckResourceAttr("data.postgresql_table.test_table", "columns.0.numeric_precision", "0"),
					resource.TestCheckResourceAttr("data.postgresql_table.test_table", "columns.0.numeric_scale", "0"),
					resource.TestCheckResourceAttr("data.postgresql_table.test_table", "columns.0.character_maximum_length", "0"),
				),
			},
		},
	})
}

// val text, test_column_one text, test_column_two text
func generateDataSourceTableConfig(dbName string) string {
	return fmt.Sprintf(`
	data "postgresql_table" "test_table" {
		database = "%s"
		schema = "test_schema"
		table = "test_table"
	}
	`, dbName)
}
