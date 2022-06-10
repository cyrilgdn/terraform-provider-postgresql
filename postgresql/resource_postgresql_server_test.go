package postgresql

import (
	"database/sql"
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
)

func TestAccPostgresqlServer_Basic(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccPreCheck(t)
			testCheckCompatibleVersion(t, featureServer)
		},
		Providers:    testAccProviders,
		CheckDestroy: testAccCheckPostgresqlServerDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccPostgresqlServerConfig,
				Check: resource.ComposeTestCheckFunc(
					testAccCheckPostgresqlServerExists("postgresql_server.myserver_postgres"),
					testAccCheckPostgresqlServerExists("postgresql_server.myserver_file"),
					testAccCheckPostgresqlServerExists("postgresql_server.myserver_with_owner"),
					testAccCheckPostgresqlServerExists("postgresql_server.myserver_with_version"),
					resource.TestCheckResourceAttr(
						"postgresql_server.myserver_postgres", "server_name", "myserver_postgres"),
					resource.TestCheckResourceAttr(
						"postgresql_server.myserver_postgres", "server_owner", "postgres"),
					resource.TestCheckResourceAttr(
						"postgresql_server.myserver_postgres", "fdw_name", "postgres_fdw"),
					resource.TestCheckResourceAttr(
						"postgresql_server.myserver_postgres", "options.host", "foo"),
					resource.TestCheckResourceAttr(
						"postgresql_server.myserver_postgres", "options.dbname", "foodb"),
					resource.TestCheckResourceAttr(
						"postgresql_server.myserver_postgres", "options.port", "5432"),
					resource.TestCheckResourceAttr(
						"postgresql_server.myserver_file", "server_name", "myserver_file"),
					resource.TestCheckResourceAttr(
						"postgresql_server.myserver_file", "server_owner", "postgres"),
					resource.TestCheckResourceAttr(
						"postgresql_server.myserver_file", "fdw_name", "file_fdw"),
					resource.TestCheckResourceAttr(
						"postgresql_server.myserver_with_owner", "server_owner", "owner"),
					resource.TestCheckResourceAttr(
						"postgresql_server.myserver_with_type", "server_type", "slave"),
					resource.TestCheckResourceAttr(
						"postgresql_server.myserver_with_version", "server_version", "1.1.1"),
				),
			},
		},
	})
}

func testAccCheckPostgresqlServerDestroy(s *terraform.State) error {
	client := testAccProvider.Meta().(*Client)

	for _, rs := range s.RootModule().Resources {
		if rs.Type != "postgresql_server" {
			continue
		}

		txn, err := startTransaction(client, "")
		if err != nil {
			return err
		}
		defer deferredRollback(txn)

		exists, err := checkServerExists(txn, rs.Primary.ID)

		if err != nil {
			return fmt.Errorf("Error checking foreign server %s", err)
		}

		if exists {
			return fmt.Errorf("Foreign Server still exists after destroy")
		}
	}

	return nil
}

func testAccCheckPostgresqlServerExists(n string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[n]
		if !ok {
			return fmt.Errorf("Resource not found: %s", n)
		}

		if rs.Primary.ID == "" {
			return fmt.Errorf("No ID is set")
		}

		serverName, ok := rs.Primary.Attributes[serverNameAttr]
		if !ok {
			return fmt.Errorf("No Attribute for server name is set")
		}

		client := testAccProvider.Meta().(*Client)
		txn, err := startTransaction(client, "")
		if err != nil {
			return err
		}
		defer deferredRollback(txn)

		exists, err := checkServerExists(txn, serverName)

		if err != nil {
			return fmt.Errorf("Error checking foreign server %s", err)
		}

		if !exists {
			return fmt.Errorf("Foreign server not found")
		}

		return nil
	}
}

func TestAccPostgresqlServer_Update(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccPreCheck(t)
			testCheckCompatibleVersion(t, featureServer)
		},
		Providers:    testAccProviders,
		CheckDestroy: testAccCheckPostgresqlServerDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccPostgresqlServerChanges1,
				Check: resource.ComposeTestCheckFunc(
					testAccCheckPostgresqlServerExists("postgresql_server.myserver_postgres"),
					resource.TestCheckResourceAttr(
						"postgresql_server.myserver_postgres", "server_name", "myserver_postgres"),
				),
			},
			{
				Config: testAccPostgresqlServerChanges2,
				Check: resource.ComposeTestCheckFunc(
					testAccCheckPostgresqlServerExists("postgresql_server.myserver_postgres"),
					resource.TestCheckResourceAttr(
						"postgresql_server.myserver_postgres", "server_name", "myserver_postgres_updated"),
				),
			},
			{
				Config: testAccPostgresqlServerChanges3,
				Check: resource.ComposeTestCheckFunc(
					testAccCheckPostgresqlServerExists("postgresql_server.myserver_postgres"),
					resource.TestCheckResourceAttr(
						"postgresql_server.myserver_postgres", "server_type", "custom"),
				),
			},
			{
				Config: testAccPostgresqlServerChanges4,
				Check: resource.ComposeTestCheckFunc(
					testAccCheckPostgresqlServerExists("postgresql_server.myserver_postgres"),
					resource.TestCheckResourceAttr(
						"postgresql_server.myserver_postgres", "server_version", "1.2.3"),
				),
			},
			{
				Config: testAccPostgresqlServerChanges5,
				Check: resource.ComposeTestCheckFunc(
					testAccCheckPostgresqlServerExists("postgresql_server.myserver_postgres"),
					resource.TestCheckResourceAttr(
						"postgresql_server.myserver_postgres", "options.host", "local"),
					resource.TestCheckResourceAttr(
						"postgresql_server.myserver_postgres", "options.dbname", "mydb"),
					resource.TestCheckResourceAttr(
						"postgresql_server.myserver_postgres", "options.port", "25432"),
				),
			},
			{
				Config: testAccPostgresqlServerChanges6,
				Check: resource.ComposeTestCheckFunc(
					testAccCheckPostgresqlServerExists("postgresql_server.myserver_postgres"),
					resource.TestCheckResourceAttr(
						"postgresql_server.myserver_postgres", "options.sslmode", "require"),
				),
			},
			{
				Config: testAccPostgresqlServerChanges7,
				Check: resource.ComposeTestCheckFunc(
					testAccCheckPostgresqlServerExists("postgresql_server.myserver_postgres"),
				),
			},
			{
				Config: testAccPostgresqlServerChanges8,
				Check: resource.ComposeTestCheckFunc(
					testAccCheckPostgresqlServerExists("postgresql_server.myserver_postgres"),
					resource.TestCheckResourceAttr(
						"postgresql_server.myserver_postgres", "options.%", "0"),
				),
			},
		},
	})
}

func checkServerExists(txn *sql.Tx, serverName string) (bool, error) {
	var _rez bool
	err := txn.QueryRow("SELECT TRUE FROM pg_foreign_server WHERE srvname=$1", serverName).Scan(&_rez)
	switch {
	case err == sql.ErrNoRows:
		return false, nil
	case err != nil:
		return false, fmt.Errorf("Error reading info about foreign server: %s", err)
	}

	return true, nil
}

func TestAccPostgresqlServer_DropCascade(t *testing.T) {
	skipIfNotAcc(t)

	var testAccPostgresqlServerConfig = `
resource "postgresql_extension" "ext_postgres_fdw" {
  name = "postgres_fdw"
}
	
resource "postgresql_server" "cascade" {
  server_name = "myserver"
  fdw_name    = "postgres_fdw"
  options = {
	host   = "foo"
	dbname = "foodb"
	port   = "5432"
  }
  drop_cascade = true

  depends_on = [postgresql_extension.ext_postgres_fdw]
}
`
	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccPreCheck(t)
			testCheckCompatibleVersion(t, featureServer)
		},
		Providers:    testAccProviders,
		CheckDestroy: testAccCheckPostgresqlServerDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccPostgresqlServerConfig,
				Check: resource.ComposeTestCheckFunc(
					testAccCheckPostgresqlServerExists("postgresql_server.cascade"),
					resource.TestCheckResourceAttr("postgresql_server.cascade", "server_name", "myserver"),
					// This will create a dependency on the server.
					testAccCreateServerDependency("myserver"),
				),
			},
		},
	})
}

func testAccCreateServerDependency(serverName string) resource.TestCheckFunc {
	return func(s *terraform.State) error {

		client := testAccProvider.Meta().(*Client)
		db, err := client.Connect()
		if err != nil {
			return err
		}
		currentUser, err := getCurrentUser(db)
		if err != nil {
			return err
		}
		_, err = db.Exec(fmt.Sprintf("CREATE USER MAPPING FOR %s SERVER %s OPTIONS (user 'admin', password 'admin');", currentUser, serverName))
		if err != nil {
			return fmt.Errorf("could not create user mapping: %s", err)
		}

		return nil
	}
}

var testAccPostgresqlServerConfig = `
resource "postgresql_extension" "ext_postgres_fdw" {
  name = "postgres_fdw"
}

resource "postgresql_extension" "ext_file_fdw" {
  name = "file_fdw"
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


resource "postgresql_server" "myserver_file" {
  server_name = "myserver_file"
  fdw_name    = "file_fdw"  
  depends_on = [postgresql_extension.ext_file_fdw]
}

resource "postgresql_role" "owner" {
  name = "owner"
}

resource "postgresql_server" "myserver_with_owner" {
  server_name  = "with_owner"
  server_owner = postgresql_role.owner.name
  fdw_name     = "postgres_fdw"
  options = {
    host   = "foo"
    dbname = "foodb"
    port   = "5432"
  }

  depends_on = [postgresql_extension.ext_postgres_fdw]
}

resource "postgresql_server" "myserver_with_type" {
  server_name = "myserver_with_type"
  server_type = "slave"
  fdw_name    = "postgres_fdw"
  options = {
    host   = "foo"
    dbname = "foodb"
    port   = "5432"
  }

  depends_on = [postgresql_extension.ext_postgres_fdw]
}


resource "postgresql_server" "myserver_with_version" {
  server_name    = "myserver_with_version"
  server_version = "1.1.1"
  fdw_name       = "postgres_fdw"
  options = {
    host   = "foo"
    dbname = "foodb"
    port   = "5432"
  }

depends_on = [postgresql_extension.ext_postgres_fdw]
}

`

var testAccPostgresqlServerChanges1 = `
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
`

var testAccPostgresqlServerChanges2 = `
resource "postgresql_extension" "ext_postgres_fdw" {
  name = "postgres_fdw"
}

resource "postgresql_server" "myserver_postgres" {
	server_name = "myserver_postgres_updated"
	fdw_name    = "postgres_fdw"
	options = {
		host   = "foo"
		dbname = "foodb"
		port   = "5432"
	}
  
	depends_on = [postgresql_extension.ext_postgres_fdw]
}
`

var testAccPostgresqlServerChanges3 = `
resource "postgresql_extension" "ext_postgres_fdw" {
	name = "postgres_fdw"
}

resource "postgresql_server" "myserver_postgres" {
	server_name = "myserver_postgres_updated"
	server_type = "custom"
	fdw_name    = "postgres_fdw"
	options = {
		host   = "foo"
		dbname = "foodb"
		port   = "5432"
	}
  
	depends_on = [postgresql_extension.ext_postgres_fdw]
}
`

var testAccPostgresqlServerChanges4 = `
resource "postgresql_extension" "ext_postgres_fdw" {
	name = "postgres_fdw"
}

resource "postgresql_server" "myserver_postgres" {
	server_name    = "myserver_postgres_updated"
	server_version = "1.2.3"
	fdw_name       = "postgres_fdw"
	options = {
		host   = "foo"
		dbname = "foodb"
		port   = "5432"
	}
  
	depends_on = [postgresql_extension.ext_postgres_fdw]
}
`

var testAccPostgresqlServerChanges5 = `
resource "postgresql_extension" "ext_postgres_fdw" {
	name = "postgres_fdw"
}

resource "postgresql_server" "myserver_postgres" {
	server_name    = "myserver_postgres_updated"
	server_version = "1.2.3"
	fdw_name       = "postgres_fdw"
	options = {
		host   = "local"
		dbname = "mydb"
		port   = "25432"
	}
  
	depends_on = [postgresql_extension.ext_postgres_fdw]
}
`

var testAccPostgresqlServerChanges6 = `
resource "postgresql_extension" "ext_postgres_fdw" {
	name = "postgres_fdw"
}

resource "postgresql_server" "myserver_postgres" {
	server_name    = "myserver_postgres_updated"
	server_version = "1.2.3"
	fdw_name       = "postgres_fdw"
	options = {
		host    = "local"
		dbname  = "mydb"
		port    = "25432"
		sslmode = "require"
	}
  
	depends_on = [postgresql_extension.ext_postgres_fdw]
}
`

var testAccPostgresqlServerChanges7 = `
resource "postgresql_extension" "ext_postgres_fdw" {
	name = "postgres_fdw"
}

resource "postgresql_server" "myserver_postgres" {
	server_name    = "myserver_postgres_updated"
	server_version = "1.2.3"
	fdw_name       = "postgres_fdw"
	options = {
		host   = "local"
		dbname = "mydb"
		port   = "25432"
	}
  
	depends_on = [postgresql_extension.ext_postgres_fdw]
}
`

var testAccPostgresqlServerChanges8 = `
resource "postgresql_extension" "ext_postgres_fdw" {
	name = "postgres_fdw"
}

resource "postgresql_server" "myserver_postgres" {
	server_name    = "myserver_postgres_updated"
	server_version = "1.2.3"
	fdw_name       = "postgres_fdw"
	depends_on = [postgresql_extension.ext_postgres_fdw]
}
`
