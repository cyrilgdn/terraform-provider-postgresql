package postgresql

import (
	"database/sql"
	"fmt"
	"testing"

	"github.com/hashicorp/terraform/helper/resource"
	"github.com/hashicorp/terraform/terraform"
)

func TestAccPostgresqlRole_Basic(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:     func() { testAccPreCheck(t) },
		Providers:    testAccProviders,
		CheckDestroy: testAccCheckPostgresqlRoleDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccPostgresqlRoleConfig,
				Check: resource.ComposeTestCheckFunc(
					testAccCheckPostgresqlRoleExists("postgresql_role.myrole2", "true"),
					resource.TestCheckResourceAttr("postgresql_role.role_with_defaults", "name", "testing_role_with_defaults"),
					resource.TestCheckResourceAttr("postgresql_role.role_with_defaults", "superuser", "false"),
					resource.TestCheckResourceAttr("postgresql_role.role_with_defaults", "create_database", "false"),
					resource.TestCheckResourceAttr("postgresql_role.role_with_defaults", "create_role", "false"),
					resource.TestCheckResourceAttr("postgresql_role.role_with_defaults", "inherit", "false"),
					resource.TestCheckResourceAttr("postgresql_role.role_with_defaults", "replication", "false"),
					resource.TestCheckResourceAttr("postgresql_role.role_with_defaults", "bypass_row_level_security", "false"),
					resource.TestCheckResourceAttr("postgresql_role.role_with_defaults", "connection_limit", "-1"),
					resource.TestCheckResourceAttr("postgresql_role.role_with_defaults", "encrypted_password", "true"),
					resource.TestCheckResourceAttr("postgresql_role.role_with_defaults", "password", ""),
					resource.TestCheckResourceAttr("postgresql_role.role_with_defaults", "valid_until", "infinity"),
					resource.TestCheckResourceAttr("postgresql_role.role_with_defaults", "skip_drop_role", "false"),
					resource.TestCheckResourceAttr("postgresql_role.role_with_defaults", "skip_reassign_owned", "false"),

					resource.TestCheckResourceAttr("postgresql_role.role_with_create_database", "name", "role_with_create_database"),
					resource.TestCheckResourceAttr("postgresql_role.role_with_create_database", "create_database", "true"),
					resource.TestCheckResourceAttr("postgresql_role.role_with_superuser", "name", "role_with_superuser"),
					resource.TestCheckResourceAttr("postgresql_role.role_with_superuser", "superuser", "true"),
				),
			},
		},
	})
}

func TestAccPostgresqlRole_Update(t *testing.T) {

	var configCreate = `
resource "postgresql_role" "update_role" {
  name = "update_role"
  login = true
  password = "toto"
  valid_until = "2019-05-04 12:00:00+00"
}
`

	var configUpdate = `
resource "postgresql_role" "update_role" {
  name = "update_role2"
  login = true
  connection_limit = 5
  password = "titi"
}
`
	resource.Test(t, resource.TestCase{
		PreCheck:     func() { testAccPreCheck(t) },
		Providers:    testAccProviders,
		CheckDestroy: testAccCheckPostgresqlRoleDestroy,
		Steps: []resource.TestStep{
			{
				Config: configCreate,
				Check: resource.ComposeTestCheckFunc(
					testAccCheckPostgresqlRoleExists("postgresql_role.update_role", "true"),
					resource.TestCheckResourceAttr("postgresql_role.update_role", "name", "update_role"),
					resource.TestCheckResourceAttr("postgresql_role.update_role", "login", "true"),
					resource.TestCheckResourceAttr("postgresql_role.update_role", "connection_limit", "-1"),
					resource.TestCheckResourceAttr("postgresql_role.update_role", "password", "toto"),
					resource.TestCheckResourceAttr("postgresql_role.update_role", "valid_until", "2019-05-04 12:00:00+00"),
					testAccCheckRoleCanLogin(t, "update_role", "toto"),
				),
			},
			{
				Config: configUpdate,
				Check: resource.ComposeTestCheckFunc(
					testAccCheckPostgresqlRoleExists("postgresql_role.update_role", "true"),
					resource.TestCheckResourceAttr("postgresql_role.update_role", "name", "update_role2"),
					resource.TestCheckResourceAttr("postgresql_role.update_role", "login", "true"),
					resource.TestCheckResourceAttr("postgresql_role.update_role", "password", "titi"),
					resource.TestCheckResourceAttr("postgresql_role.update_role", "valid_until", "infinity"),
					testAccCheckRoleCanLogin(t, "update_role2", "titi"),
				),
			},
		},
	})
}

func testAccCheckPostgresqlRoleDestroy(s *terraform.State) error {
	client := testAccProvider.Meta().(*Client)

	for _, rs := range s.RootModule().Resources {
		if rs.Type != "postgresql_role" {
			continue
		}

		exists, err := checkRoleExists(client, rs.Primary.ID)

		if err != nil {
			return fmt.Errorf("Error checking role %s", err)
		}

		if exists {
			return fmt.Errorf("Role still exists after destroy")
		}
	}

	return nil
}

func testAccCheckPostgresqlRoleExists(n string, canLogin string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[n]
		if !ok {
			return fmt.Errorf("Resource not found: %s", n)
		}

		if rs.Primary.ID == "" {
			return fmt.Errorf("No ID is set")
		}

		actualCanLogin := rs.Primary.Attributes["login"]
		if actualCanLogin != canLogin {
			return fmt.Errorf("Wrong value for login expected %s got %s", canLogin, actualCanLogin)
		}

		client := testAccProvider.Meta().(*Client)
		exists, err := checkRoleExists(client, rs.Primary.ID)

		if err != nil {
			return fmt.Errorf("Error checking role %s", err)
		}

		if !exists {
			return fmt.Errorf("Role not found")
		}

		return nil
	}
}

func checkRoleExists(client *Client, roleName string) (bool, error) {
	var _rez int
	err := client.DB().QueryRow("SELECT 1 from pg_roles d WHERE rolname=$1", roleName).Scan(&_rez)
	switch {
	case err == sql.ErrNoRows:
		return false, nil
	case err != nil:
		return false, fmt.Errorf("Error reading info about role: %s", err)
	}

	return true, nil
}

func testAccCheckRoleCanLogin(t *testing.T, role, password string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		config := getTestConfig(t)
		config.Username = role
		config.Password = password
		db, err := sql.Open("postgres", config.connStr())
		if err != nil {
			return fmt.Errorf("could not open SQL connection: %v", err)
		}
		if err := db.Ping(); err != nil {
			return fmt.Errorf("could not connect as role %s: %v", role, err)
		}
		return nil
	}
}

var testAccPostgresqlRoleConfig = `
resource "postgresql_role" "myrole2" {
  name = "myrole2"
  login = true
}

resource "postgresql_role" "role_with_pwd" {
  name = "role_with_pwd"
  login = true
  password = "mypass"
}

resource "postgresql_role" "role_with_pwd_encr" {
  name = "role_with_pwd_encr"
  login = true
  password = "mypass"
  encrypted = true
}

resource "postgresql_role" "role_simple" {
  name = "role_simple"
}

resource "postgresql_role" "role_with_defaults" {
  name = "testing_role_with_defaults"
  superuser = false
  create_database = false
  create_role = false
  inherit = false
  login = false
  replication = false
  bypass_row_level_security = false
  connection_limit = -1
  encrypted_password = true
  password = ""
  skip_drop_role = false
  skip_reassign_owned = false
  valid_until = "infinity"
}

resource "postgresql_role" "role_with_create_database" {
  name = "role_with_create_database"
  create_database = true
}

resource "postgresql_role" "role_with_superuser" {
  name = "role_with_superuser"
  superuser = true
  login = true
  password = "mypass"
}
`
