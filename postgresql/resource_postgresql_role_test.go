package postgresql

import (
	"database/sql"
	"fmt"
	"reflect"
	"sort"
	"testing"

	"github.com/hashicorp/terraform/helper/resource"
	"github.com/hashicorp/terraform/terraform"
)

func TestAccPostgresqlRole_Basic(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccPreCheck(t)
			testCheckCompatibleVersion(t, featurePrivileges)
		},
		Providers:    testAccProviders,
		CheckDestroy: testAccCheckPostgresqlRoleDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccPostgresqlRoleConfig,
				Check: resource.ComposeTestCheckFunc(
					testAccCheckPostgresqlRoleExists("myrole2", nil),
					resource.TestCheckResourceAttr("postgresql_role.myrole2", "name", "myrole2"),
					resource.TestCheckResourceAttr("postgresql_role.myrole2", "login", "true"),
					resource.TestCheckResourceAttr("postgresql_role.myrole2", "roles.#", "0"),

					testAccCheckPostgresqlRoleExists("role_default", nil),
					resource.TestCheckResourceAttr("postgresql_role.role_with_defaults", "name", "role_default"),
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
					resource.TestCheckResourceAttr("postgresql_role.role_with_defaults", "roles.#", "0"),

					testAccCheckPostgresqlRoleExists("sub_role", []string{"myrole2", "role_simple"}),
					resource.TestCheckResourceAttr("postgresql_role.sub_role", "name", "sub_role"),
					resource.TestCheckResourceAttr("postgresql_role.sub_role", "roles.#", "2"),

					// The int part in the attr name is the schema.HashString of the value.
					resource.TestCheckResourceAttr("postgresql_role.sub_role", "roles.719783566", "myrole2"),
					resource.TestCheckResourceAttr("postgresql_role.sub_role", "roles.1784536243", "role_simple"),
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
  valid_until = "2099-05-04 12:00:00+00"
}
`

	var configUpdate = `
resource "postgresql_role" "group_role" {
  name = "group_role"
}

resource "postgresql_role" "update_role" {
  name = "update_role2"
  login = true
  connection_limit = 5
  password = "titi"
  roles = ["${postgresql_role.group_role.name}"]
}
`
	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccPreCheck(t)
			testCheckCompatibleVersion(t, featurePrivileges)
		},
		Providers:    testAccProviders,
		CheckDestroy: testAccCheckPostgresqlRoleDestroy,
		Steps: []resource.TestStep{
			{
				Config: configCreate,
				Check: resource.ComposeTestCheckFunc(
					testAccCheckPostgresqlRoleExists("update_role", []string{}),
					resource.TestCheckResourceAttr("postgresql_role.update_role", "name", "update_role"),
					resource.TestCheckResourceAttr("postgresql_role.update_role", "login", "true"),
					resource.TestCheckResourceAttr("postgresql_role.update_role", "connection_limit", "-1"),
					resource.TestCheckResourceAttr("postgresql_role.update_role", "password", "toto"),
					resource.TestCheckResourceAttr("postgresql_role.update_role", "valid_until", "2099-05-04 12:00:00+00"),
					resource.TestCheckResourceAttr("postgresql_role.update_role", "roles.#", "0"),
					testAccCheckRoleCanLogin(t, "update_role", "toto"),
				),
			},
			{
				Config: configUpdate,
				Check: resource.ComposeTestCheckFunc(
					testAccCheckPostgresqlRoleExists("update_role2", []string{"group_role"}),
					resource.TestCheckResourceAttr(
						"postgresql_role.update_role", "name", "update_role2",
					),
					resource.TestCheckResourceAttr("postgresql_role.update_role", "connection_limit", "5"),
					resource.TestCheckResourceAttr("postgresql_role.update_role", "login", "true"),
					resource.TestCheckResourceAttr("postgresql_role.update_role", "password", "titi"),
					resource.TestCheckResourceAttr("postgresql_role.update_role", "valid_until", "infinity"),
					resource.TestCheckResourceAttr("postgresql_role.update_role", "roles.#", "1"),
					// The int part in the attr name is the schema.HashString of the value.
					resource.TestCheckResourceAttr(
						"postgresql_role.update_role", "roles.2117325082", "group_role",
					),
					testAccCheckRoleCanLogin(t, "update_role2", "titi"),
				),
			},
			// apply again the first one to tests the granted role is correctly revoked
			{
				Config: configCreate,
				Check: resource.ComposeTestCheckFunc(
					testAccCheckPostgresqlRoleExists("update_role", []string{}),
					resource.TestCheckResourceAttr("postgresql_role.update_role", "name", "update_role"),
					resource.TestCheckResourceAttr("postgresql_role.update_role", "login", "true"),
					resource.TestCheckResourceAttr("postgresql_role.update_role", "connection_limit", "-1"),
					resource.TestCheckResourceAttr("postgresql_role.update_role", "password", "toto"),
					resource.TestCheckResourceAttr("postgresql_role.update_role", "roles.#", "0"),
					testAccCheckRoleCanLogin(t, "update_role", "toto"),
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

func testAccCheckPostgresqlRoleExists(roleName string, grantedRoles []string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		client := testAccProvider.Meta().(*Client)

		exists, err := checkRoleExists(client, roleName)
		if err != nil {
			return fmt.Errorf("Error checking role %s", err)
		}

		if !exists {
			return fmt.Errorf("Role not found")
		}

		if grantedRoles != nil {
			return checkGrantedRoles(client, roleName, grantedRoles)
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
		db, err := sql.Open("postgres", config.connStr("postgres"))
		if err != nil {
			return fmt.Errorf("could not open SQL connection: %v", err)
		}
		if err := db.Ping(); err != nil {
			return fmt.Errorf("could not connect as role %s: %v", role, err)
		}
		return nil
	}
}

func checkGrantedRoles(client *Client, roleName string, expectedRoles []string) error {
	rows, err := client.DB().Query(
		"SELECT role_name FROM information_schema.applicable_roles WHERE grantee=$1 ORDER BY role_name",
		roleName,
	)
	if err != nil {
		return fmt.Errorf("Error reading granted roles: %v", err)
	}
	defer rows.Close()

	grantedRoles := []string{}
	for rows.Next() {
		var grantedRole string
		if err := rows.Scan(&grantedRole); err != nil {
			return fmt.Errorf("Error scanning granted role: %v", err)
		}
		grantedRoles = append(grantedRoles, grantedRole)
	}

	sort.Strings(expectedRoles)
	if !reflect.DeepEqual(grantedRoles, expectedRoles) {
		return fmt.Errorf(
			"Role %s is not a members of the expected list of roles. expected %v - got %v",
			roleName, expectedRoles, grantedRoles,
		)
	}
	return nil
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
  name = "role_default"
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

resource "postgresql_role" "sub_role" {
	name = "sub_role"
	roles = [
		"${postgresql_role.myrole2.id}",
		"${postgresql_role.role_simple.id}",
	]
}


resource "postgresql_role" "role_with_superuser" {
  name = "role_with_superuser"
  superuser = true
  login = true
  password = "mypass"
}
`
