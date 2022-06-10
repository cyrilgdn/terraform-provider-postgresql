package postgresql

import (
	"database/sql"
	"fmt"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
)

func TestAccPostgresqlUserMapping_Basic(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccPreCheck(t)
			testCheckCompatibleVersion(t, featureServer)
		},
		Providers:    testAccProviders,
		CheckDestroy: testAccCheckPostgresqlUserMappingDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccPostgresqlUserMappingConfig,
				Check: resource.ComposeTestCheckFunc(
					testAccCheckPostgresqlUserMappingExists("postgresql_user_mapping.remote"),
					resource.TestCheckResourceAttr(
						"postgresql_user_mapping.remote", "server_name", "myserver_postgres"),
					resource.TestCheckResourceAttr(
						"postgresql_user_mapping.remote", "user_name", "remote"),
					resource.TestCheckResourceAttr(
						"postgresql_user_mapping.remote", "options.user", "admin"),
					resource.TestCheckResourceAttr(
						"postgresql_user_mapping.remote", "options.password", "pass"),
				),
			},
		},
	})
}

func TestAccPostgresqlUserMapping_Update(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccPreCheck(t)
			testCheckCompatibleVersion(t, featureServer)
		},
		Providers:    testAccProviders,
		CheckDestroy: testAccCheckPostgresqlUserMappingDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccPostgresqlUserMappingConfig,
				Check: resource.ComposeTestCheckFunc(
					testAccCheckPostgresqlServerExists("postgresql_user_mapping.remote"),
					resource.TestCheckResourceAttr(
						"postgresql_user_mapping.remote", "server_name", "myserver_postgres"),
					resource.TestCheckResourceAttr(
						"postgresql_user_mapping.remote", "options.password", "pass"),
				),
			},
			{
				Config: testAccPostgresqlUserMappingChanges2,
				Check: resource.ComposeTestCheckFunc(
					testAccCheckPostgresqlServerExists("postgresql_user_mapping.remote"),
					resource.TestCheckResourceAttr(
						"postgresql_user_mapping.remote", "options.password", "passUpdated"),
				),
			},
			{
				Config: testAccPostgresqlUserMappingChanges3,
				Check: resource.ComposeTestCheckFunc(
					testAccCheckPostgresqlServerExists("postgresql_user_mapping.remote"),
					resource.TestCheckResourceAttr(
						"postgresql_user_mapping.remote", "options.%", "0"),
				),
			},
		},
	})
}

func checkUserMappingExists(txn *sql.Tx, username string, serverName string) (bool, error) {
	var _rez bool
	err := txn.QueryRow("SELECT TRUE FROM pg_user_mappings WHERE usename = $1 AND srvname = $2", username, serverName).Scan(&_rez)
	switch {
	case err == sql.ErrNoRows:
		return false, nil
	case err != nil:
		return false, fmt.Errorf("Error reading info about user mapping: %s", err)
	}

	return true, nil
}

func testAccCheckPostgresqlUserMappingDestroy(s *terraform.State) error {
	client := testAccProvider.Meta().(*Client)

	for _, rs := range s.RootModule().Resources {
		if rs.Type != "postgresql_user_mapping" {
			continue
		}

		txn, err := startTransaction(client, "")
		if err != nil {
			return err
		}
		defer deferredRollback(txn)

		splitted := strings.Split(rs.Primary.ID, ".")
		exists, err := checkUserMappingExists(txn, splitted[0], splitted[1])

		if err != nil {
			return fmt.Errorf("Error checking user mapping %s", err)
		}

		if exists {
			return fmt.Errorf("User mapping still exists after destroy")
		}
	}

	return nil
}

func testAccCheckPostgresqlUserMappingExists(n string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[n]
		if !ok {
			return fmt.Errorf("Resource not found: %s", n)
		}

		if rs.Primary.ID == "" {
			return fmt.Errorf("No ID is set")
		}

		username, ok := rs.Primary.Attributes[userMappingUserNameAttr]
		if !ok {
			return fmt.Errorf("No Attribute for username is set")
		}

		serverName, ok := rs.Primary.Attributes[userMappingServerNameAttr]
		if !ok {
			return fmt.Errorf("No Attribute for server name is set")
		}

		client := testAccProvider.Meta().(*Client)
		txn, err := startTransaction(client, "")
		if err != nil {
			return err
		}
		defer deferredRollback(txn)

		exists, err := checkUserMappingExists(txn, username, serverName)

		if err != nil {
			return fmt.Errorf("Error checking user mapping %s", err)
		}

		if !exists {
			return fmt.Errorf("User mapping not found")
		}

		return nil
	}
}

var testAccPostgresqlUserMappingConfig = `
resource "postgresql_extension" "ext_postgres_fdw" {
  name = "postgres_fdw"
}

resource "postgresql_server" "myserver_postgres" {
  server_name = "myserver_postgres"
  fdw_name    = "postgres_fdw"
  options = {
    host   = "foo"
    dbname = "foodb"
    port   = "5432"
  }

  depends_on = [postgresql_extension.ext_postgres_fdw]
}

resource "postgresql_role" "remote" {
  name = "remote"
}

resource "postgresql_user_mapping" "remote" {
  server_name = postgresql_server.myserver_postgres.server_name
  user_name   = postgresql_role.remote.name
  options = {
    user = "admin"
    password = "pass"
  }
}
`

var testAccPostgresqlUserMappingChanges2 = `
resource "postgresql_extension" "ext_postgres_fdw" {
	name = "postgres_fdw"
  }
  
  resource "postgresql_server" "myserver_postgres" {
	server_name = "myserver_postgres"
	fdw_name    = "postgres_fdw"
	options = {
	  host   = "foo"
	  dbname = "foodb"
	  port   = "5432"
	}
  
	depends_on = [postgresql_extension.ext_postgres_fdw]
  }
  
  resource "postgresql_role" "remote" {
	name = "remote"
  }
  
  resource "postgresql_user_mapping" "remote" {
	server_name = postgresql_server.myserver_postgres.server_name
	user_name   = postgresql_role.remote.name
	options = {
	  user = "admin"
	  password = "passUpdated"
	}
  }
`

var testAccPostgresqlUserMappingChanges3 = `
resource "postgresql_extension" "ext_postgres_fdw" {
	name = "postgres_fdw"
  }
  
  resource "postgresql_server" "myserver_postgres" {
	server_name = "myserver_postgres"
	fdw_name    = "postgres_fdw"
	options = {
	  host   = "foo"
	  dbname = "foodb"
	  port   = "5432"
	}
  
	depends_on = [postgresql_extension.ext_postgres_fdw]
  }
  
  resource "postgresql_role" "remote" {
	name = "remote"
  }
  
  resource "postgresql_user_mapping" "remote" {
	server_name = postgresql_server.myserver_postgres.server_name
	user_name   = postgresql_role.remote.name
  }
`
