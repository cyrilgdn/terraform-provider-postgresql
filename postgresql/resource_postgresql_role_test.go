package postgresql

import (
	"database/sql"
	"fmt"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
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
					testAccCheckPostgresqlRoleExists("myrole2", nil, nil),
					resource.TestCheckResourceAttr("postgresql_role.myrole2", "name", "myrole2"),
					resource.TestCheckResourceAttr("postgresql_role.myrole2", "login", "true"),
					resource.TestCheckResourceAttr("postgresql_role.myrole2", "roles.#", "0"),

					testAccCheckPostgresqlRoleExists("role_default", nil, nil),
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
					resource.TestCheckResourceAttr("postgresql_role.role_with_defaults", "statement_timeout", "0"),
					resource.TestCheckResourceAttr("postgresql_role.role_with_defaults", "idle_in_transaction_session_timeout", "0"),
					resource.TestCheckResourceAttr("postgresql_role.role_with_defaults", "assume_role", ""),

					resource.TestCheckResourceAttr("postgresql_role.role_with_create_database", "name", "role_with_create_database"),
					resource.TestCheckResourceAttr("postgresql_role.role_with_create_database", "create_database", "true"),

					testAccCheckPostgresqlRoleExists("sub_role", []string{"myrole2", "role_simple"}, nil),
					resource.TestCheckResourceAttr("postgresql_role.sub_role", "name", "sub_role"),
					resource.TestCheckResourceAttr("postgresql_role.sub_role", "roles.#", "2"),
					resource.TestCheckResourceAttr("postgresql_role.sub_role", "roles.0", "myrole2"),
					resource.TestCheckResourceAttr("postgresql_role.sub_role", "roles.1", "role_simple"),

					testAccCheckPostgresqlRoleExists("role_with_search_path", nil, []string{"bar", "foo-with-hyphen"}),
				),
			},
		},
	})
}

// Test creating a superuser role.
func TestAccPostgresqlRole_Superuser(t *testing.T) {

	roleConfig := `
resource "postgresql_role" "role_with_superuser" {
  name = "role_with_superuser"
  superuser = true
  login = true
  password = "mypass"
}`

	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccPreCheck(t)
			testCheckCompatibleVersion(t, featurePrivileges)
			// Need to a be a superuser to create a superuser
			testSuperuserPreCheck(t)
		},
		Providers:    testAccProviders,
		CheckDestroy: testAccCheckPostgresqlRoleDestroy,
		Steps: []resource.TestStep{
			{
				Config: roleConfig,
				Check: resource.ComposeTestCheckFunc(
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
  search_path = ["mysearchpath"]
  statement_timeout = 30000
  idle_in_transaction_session_timeout = 60000
  assume_role = "${postgresql_role.group_role.name}"
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
					testAccCheckPostgresqlRoleExists("update_role", []string{}, nil),
					resource.TestCheckResourceAttr("postgresql_role.update_role", "name", "update_role"),
					resource.TestCheckResourceAttr("postgresql_role.update_role", "login", "true"),
					resource.TestCheckResourceAttr("postgresql_role.update_role", "connection_limit", "-1"),
					resource.TestCheckResourceAttr("postgresql_role.update_role", "password", "toto"),
					resource.TestCheckResourceAttr("postgresql_role.update_role", "valid_until", "2099-05-04 12:00:00+00"),
					resource.TestCheckResourceAttr("postgresql_role.update_role", "roles.#", "0"),
					resource.TestCheckResourceAttr("postgresql_role.update_role", "search_path.#", "0"),
					resource.TestCheckResourceAttr("postgresql_role.update_role", "statement_timeout", "0"),
					resource.TestCheckResourceAttr("postgresql_role.update_role", "idle_in_transaction_session_timeout", "0"),
					resource.TestCheckResourceAttr("postgresql_role.update_role", "assume_role", ""),
					testAccCheckRoleCanLogin(t, "update_role", "toto"),
				),
			},
			{
				Config: configUpdate,
				Check: resource.ComposeTestCheckFunc(
					testAccCheckPostgresqlRoleExists("update_role2", []string{"group_role"}, nil),
					resource.TestCheckResourceAttr(
						"postgresql_role.update_role", "name", "update_role2",
					),
					resource.TestCheckResourceAttr("postgresql_role.update_role", "connection_limit", "5"),
					resource.TestCheckResourceAttr("postgresql_role.update_role", "login", "true"),
					resource.TestCheckResourceAttr("postgresql_role.update_role", "password", "titi"),
					resource.TestCheckResourceAttr("postgresql_role.update_role", "valid_until", "infinity"),
					resource.TestCheckResourceAttr("postgresql_role.update_role", "roles.#", "1"),
					resource.TestCheckResourceAttr("postgresql_role.update_role", "roles.0", "group_role"),
					resource.TestCheckResourceAttr("postgresql_role.update_role", "search_path.#", "1"),
					resource.TestCheckResourceAttr("postgresql_role.update_role", "search_path.0", "mysearchpath"),
					resource.TestCheckResourceAttr("postgresql_role.update_role", "statement_timeout", "30000"),
					resource.TestCheckResourceAttr("postgresql_role.update_role", "idle_in_transaction_session_timeout", "60000"),
					resource.TestCheckResourceAttr("postgresql_role.update_role", "assume_role", "group_role"),
					testAccCheckRoleCanLogin(t, "update_role2", "titi"),
				),
			},
			// apply the first one again to test that the granted role is correctly
			// revoked and the search path has been reset to default.
			{
				Config: configCreate,
				Check: resource.ComposeTestCheckFunc(
					testAccCheckPostgresqlRoleExists("update_role", []string{}, nil),
					resource.TestCheckResourceAttr("postgresql_role.update_role", "name", "update_role"),
					resource.TestCheckResourceAttr("postgresql_role.update_role", "login", "true"),
					resource.TestCheckResourceAttr("postgresql_role.update_role", "connection_limit", "-1"),
					resource.TestCheckResourceAttr("postgresql_role.update_role", "password", "toto"),
					resource.TestCheckResourceAttr("postgresql_role.update_role", "roles.#", "0"),
					resource.TestCheckResourceAttr("postgresql_role.update_role", "search_path.#", "0"),
					resource.TestCheckResourceAttr("postgresql_role.update_role", "statement_timeout", "0"),
					resource.TestCheckResourceAttr("postgresql_role.update_role", "idle_in_transaction_session_timeout", "0"),
					resource.TestCheckResourceAttr("postgresql_role.update_role", "assume_role", ""),
					testAccCheckRoleCanLogin(t, "update_role", "toto"),
				),
			},
		},
	})
}

func TestAccPostgresqlRole_ConfigurationParameters(t *testing.T) {

	var configRoles = `
resource "postgresql_role" "role" {
  name = "role1"
  %s
}

resource "postgresql_role" "role_created_with_params" {
  name = "role2"

  parameter {
    name  = "client_min_messages"
    value = "debug"
  }
}
`
	configParameterA := `
  parameter {
    name  = "client_min_messages"
    value = "%s"
  }
`
	configParameterB := `
  parameter {
    name  = "maintenance_work_mem"
    value = "10000"
    quote = false
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
				Config: fmt.Sprintf(configRoles, ""),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("postgresql_role.role", "name", "role1"),
					resource.TestCheckResourceAttr("postgresql_role.role", "parameter.#", "0"),
					resource.TestCheckResourceAttr("postgresql_role.role_created_with_params", "name", "role2"),
					resource.TestCheckResourceAttr("postgresql_role.role_created_with_params", "parameter.#", "1"),
					resource.TestCheckResourceAttr("postgresql_role.role_created_with_params", "parameter.0.name", "client_min_messages"),
					resource.TestCheckResourceAttr("postgresql_role.role_created_with_params", "parameter.0.value", "debug"),
					testAccCheckRoleHasConfigurationParameters("role1", map[string]string{}),
					testAccCheckRoleHasConfigurationParameters("role2", map[string]string{"client_min_messages": "debug"}),
				),
			},
			{
				Config: fmt.Sprintf(configRoles, fmt.Sprintf(configParameterA, "notice")+configParameterB),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("postgresql_role.role", "name", "role1"),
					resource.TestCheckResourceAttr("postgresql_role.role", "parameter.#", "2"),
					resource.TestCheckResourceAttr("postgresql_role.role", "parameter.0.name", "client_min_messages"),
					resource.TestCheckResourceAttr("postgresql_role.role", "parameter.0.value", "notice"),
					resource.TestCheckResourceAttr("postgresql_role.role", "parameter.1.name", "maintenance_work_mem"),
					resource.TestCheckResourceAttr("postgresql_role.role", "parameter.1.value", "10000"),
					resource.TestCheckResourceAttr("postgresql_role.role_created_with_params", "name", "role2"),
					resource.TestCheckResourceAttr("postgresql_role.role_created_with_params", "parameter.#", "1"),
					testAccCheckRoleHasConfigurationParameters("role1", map[string]string{
						"client_min_messages":  "notice",
						"maintenance_work_mem": "10000",
					}),
				),
			},
			{
				Config: fmt.Sprintf(configRoles, fmt.Sprintf(configParameterA, "error")),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("postgresql_role.role", "name", "role1"),
					resource.TestCheckResourceAttr("postgresql_role.role", "parameter.#", "1"),
					resource.TestCheckResourceAttr("postgresql_role.role", "parameter.0.name", "client_min_messages"),
					resource.TestCheckResourceAttr("postgresql_role.role", "parameter.0.value", "error"),
					resource.TestCheckResourceAttr("postgresql_role.role_created_with_params", "name", "role2"),
					resource.TestCheckResourceAttr("postgresql_role.role_created_with_params", "parameter.#", "1"),
					testAccCheckRoleHasConfigurationParameters("role1", map[string]string{"client_min_messages": "error"}),
				),
			},
			{
				Config: fmt.Sprintf(configRoles, ""),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("postgresql_role.role", "name", "role1"),
					resource.TestCheckResourceAttr("postgresql_role.role", "parameter.#", "0"),
					resource.TestCheckResourceAttr("postgresql_role.role_created_with_params", "name", "role2"),
					resource.TestCheckResourceAttr("postgresql_role.role_created_with_params", "parameter.#", "1"),
					resource.TestCheckResourceAttr("postgresql_role.role_created_with_params", "parameter.0.name", "client_min_messages"),
					resource.TestCheckResourceAttr("postgresql_role.role_created_with_params", "parameter.0.value", "debug"),
					testAccCheckRoleHasConfigurationParameters("role1", map[string]string{}),
					testAccCheckRoleHasConfigurationParameters("role2", map[string]string{"client_min_messages": "debug"}),
				),
			},
		},
	})
}

func TestAccPostgresqlRole_ConfigurationParameters_WithExplicitParameterAttrs(t *testing.T) {
	var configRole = `
resource "postgresql_role" "role" {
  name = "role1"
  search_path = ["here","there"]
  idle_in_transaction_session_timeout = 300
  statement_timeout = 100
  assume_role = "other_role"
  %s
}
`
	configParameterA := `
  parameter {
    name  = "client_min_messages"
    value = "%s"
  }
`
	configParameterB := `
  parameter {
    name  = "maintenance_work_mem"
    value = "10000"
	quote = false
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
				Config: fmt.Sprintf(configRole, fmt.Sprintf(configParameterA, "debug")),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("postgresql_role.role", "name", "role1"),
					resource.TestCheckResourceAttr("postgresql_role.role", "parameter.#", "1"),
					resource.TestCheckResourceAttr("postgresql_role.role", "parameter.0.name", "client_min_messages"),
					resource.TestCheckResourceAttr("postgresql_role.role", "parameter.0.value", "debug"),
					testAccCheckRoleHasConfigurationParameters("role1", map[string]string{"client_min_messages": "debug"}),
				),
			},
			{
				Config: fmt.Sprintf(configRole, fmt.Sprintf(configParameterA, "notice")+configParameterB),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("postgresql_role.role", "name", "role1"),
					resource.TestCheckResourceAttr("postgresql_role.role", "parameter.#", "2"),
					resource.TestCheckResourceAttr("postgresql_role.role", "parameter.0.name", "client_min_messages"),
					resource.TestCheckResourceAttr("postgresql_role.role", "parameter.0.value", "notice"),
					resource.TestCheckResourceAttr("postgresql_role.role", "parameter.1.name", "maintenance_work_mem"),
					resource.TestCheckResourceAttr("postgresql_role.role", "parameter.1.value", "10000"),
					testAccCheckRoleHasConfigurationParameters("role1", map[string]string{
						"client_min_messages":  "notice",
						"maintenance_work_mem": "10000",
					}),
				),
			},
			{
				Config: fmt.Sprintf(configRole, fmt.Sprintf(configParameterA, "error")),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("postgresql_role.role", "name", "role1"),
					resource.TestCheckResourceAttr("postgresql_role.role", "parameter.#", "1"),
					resource.TestCheckResourceAttr("postgresql_role.role", "parameter.0.name", "client_min_messages"),
					resource.TestCheckResourceAttr("postgresql_role.role", "parameter.0.value", "error"),
					testAccCheckRoleHasConfigurationParameters("role1", map[string]string{"client_min_messages": "error"}),
				),
			},
			{
				Config: fmt.Sprintf(configRole, ""),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("postgresql_role.role", "name", "role1"),
					resource.TestCheckResourceAttr("postgresql_role.role", "parameter.#", "0"),
					testAccCheckRoleHasConfigurationParameters("role1", map[string]string{}),
				),
			},
		},
	})
}

// Test to create a role with admin user (usually postgres) granted to it
// There were a bug on RDS like setup (with a non-superuser postgres role)
// where it couldn't delete the role in this case.
func TestAccPostgresqlRole_AdminGranted(t *testing.T) {
	admin := os.Getenv("PGUSER")
	if admin == "" {
		admin = "postgres"
	}

	roleConfig := fmt.Sprintf(`
resource "postgresql_role" "test_role" {
  name  = "test_role"
  roles = [
	  "%s"
  ]
}`, admin)

	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccPreCheck(t)
			testCheckCompatibleVersion(t, featurePrivileges)
		},
		Providers:    testAccProviders,
		CheckDestroy: testAccCheckPostgresqlRoleDestroy,
		Steps: []resource.TestStep{
			{
				Config: roleConfig,
				Check: resource.ComposeTestCheckFunc(
					testAccCheckPostgresqlRoleExists("test_role", []string{admin}, nil),
					resource.TestCheckResourceAttr("postgresql_role.test_role", "name", "test_role"),
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

func testAccCheckPostgresqlRoleExists(roleName string, grantedRoles []string, searchPath []string) resource.TestCheckFunc {
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
			if err := checkGrantedRoles(client, roleName, grantedRoles); err != nil {
				return err
			}
		}

		if searchPath != nil {
			if err := checkSearchPath(client, roleName, searchPath); err != nil {
				return err
			}
		}
		return nil
	}
}

func checkRoleExists(client *Client, roleName string) (bool, error) {
	db, err := client.Connect()
	if err != nil {
		return false, err
	}
	var _rez int
	err = db.QueryRow("SELECT 1 from pg_roles d WHERE rolname=$1", roleName).Scan(&_rez)
	switch {
	case err == sql.ErrNoRows:
		return false, nil
	case err != nil:
		return false, fmt.Errorf("Error reading info about role: %s", err)
	}

	return true, nil
}

func testAccCheckRoleHasConfigurationParameters(roleName string, parameters map[string]string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		client := testAccProvider.Meta().(*Client)
		db, err := client.Connect()
		if err != nil {
			return err
		}
		rows, err := db.Query("SELECT UNNEST(rolconfig) FROM pg_roles WHERE rolname=$1", roleName)
		if err != nil {
			return err
		}
		setParameters := make(map[string]string)
		for rows.Next() {
			var param string
			if err := rows.Scan(&param); err != nil {
				return err
			}
			split := strings.Split(param, "=")
			if !sliceContainsStr(ignoredRoleConfigurationParameters, split[0]) {
				setParameters[split[0]] = split[1]
			}
		}
		if len(parameters) != len(setParameters) {
			return fmt.Errorf("expected role %s to have %d configuration parameters, found %d", roleName, len(parameters), len(setParameters))
		}
		for k := range parameters {
			if parameters[k] != setParameters[k] {
				return fmt.Errorf(
					"expected configuration parameter %s for role %s to have value \"%s\", found \"%s\"",
					k, roleName, parameters[k], setParameters[k])
			}
		}
		return nil
	}
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
	db, err := client.Connect()
	if err != nil {
		return err
	}

	rows, err := db.Query(
		"SELECT pg_get_userbyid(roleid) as rolname from pg_auth_members WHERE pg_get_userbyid(member) = $1 ORDER BY rolname",
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
			"Role %s is not a member of the expected list of roles. expected %v - got %v",
			roleName, expectedRoles, grantedRoles,
		)
	}
	return nil
}

func checkSearchPath(client *Client, roleName string, expectedSearchPath []string) error {
	db, err := client.Connect()
	if err != nil {
		return err
	}

	var searchPathStr string
	err = db.QueryRow(
		"SELECT (pg_options_to_table(rolconfig)).option_value FROM pg_roles WHERE rolname=$1;",
		roleName,
	).Scan(&searchPathStr)

	// The query returns ErrNoRows if the search path hasn't been altered.
	if err != nil && err == sql.ErrNoRows {
		searchPathStr = "\"$user\", public"
	} else if err != nil {
		return fmt.Errorf("Error reading search_path: %v", err)
	}

	searchPath := strings.Split(searchPathStr, ", ")
	for i := range searchPath {
		searchPath[i] = strings.Trim(searchPath[i], `"`)
	}
	sort.Strings(expectedSearchPath)
	if !reflect.DeepEqual(searchPath, expectedSearchPath) {
		return fmt.Errorf(
			"search_path is not equal to expected value. expected %v - got %v",
			expectedSearchPath, searchPath,
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
  encrypted_password = true
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
  valid_until = "infinity"
  statement_timeout = 0
  idle_in_transaction_session_timeout = 0
  assume_role = ""
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

resource "postgresql_role" "role_with_search_path" {
  name = "role_with_search_path"
  search_path = ["bar", "foo-with-hyphen"]
}
`
