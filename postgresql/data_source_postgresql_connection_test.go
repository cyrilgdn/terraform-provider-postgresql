package postgresql

import (
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"os"
	"testing"
)

func TestAccPostgresqlDataSourceConnection(t *testing.T) {
	skipIfNotAcc(t)

	testAccPostgresqlDataSourceTablesDatabaseConfig := generateDataSourceConnectionConfig()

	resource.Test(t, resource.TestCase{
		PreCheck:  func() { testAccPreCheck(t) },
		Providers: testAccProviders,
		Steps: []resource.TestStep{
			{
				Config: testAccPostgresqlDataSourceTablesDatabaseConfig,
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("data.postgresql_connection.current1", "host", os.Getenv("PGHOST")),
					resource.TestCheckResourceAttr("data.postgresql_connection.current1", "port", os.Getenv("PGPORT")),
					resource.TestCheckResourceAttr("data.postgresql_connection.current1", "scheme", "postgres"),
					resource.TestCheckResourceAttr("data.postgresql_connection.current1", "username", os.Getenv("PGUSER")),
					resource.TestCheckResourceAttr("data.postgresql_connection.current1", "database_username", ""),
					resource.TestCheckResourceAttrSet("data.postgresql_connection.current1", "version"),
					resource.TestCheckResourceAttr("data.postgresql_connection.current1", "database", "postgres"),
				),
			},
		},
	})
}

func generateDataSourceConnectionConfig() string {
	return `	
	data "postgresql_connection" "current1" {
	}
`
}
