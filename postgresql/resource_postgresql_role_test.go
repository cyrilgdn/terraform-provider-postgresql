package postgresql

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"reflect"
	"regexp"
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

func TestAccPostgresqlRole_CreateRoleSelfGrant(t *testing.T) {
	var configCreate = `
resource "postgresql_role" "role_with_createrole_self_grant" {
  name = "role_with_createrole_self_grant"
  parameters = {
    createrole_self_grant = "set, inherit"
  }
}
`
	var configUpdate = `
resource "postgresql_role" "role_with_createrole_self_grant" {
  name = "role_with_createrole_self_grant"
  parameters = {
    createrole_self_grant = "set"
  }
}
`
	var configReset = `
resource "postgresql_role" "role_with_createrole_self_grant" {
  name = "role_with_createrole_self_grant"
  parameters = {}
}
`
	var configNoParams = `
resource "postgresql_role" "role_with_createrole_self_grant" {
  name = "role_with_createrole_self_grant"
}
`
	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccPreCheck(t)
			testCheckCompatibleVersion(t, featurePrivileges)
			testCheckCompatibleVersion(t, featureCreateRoleSelfGrant)
		},
		Providers:    testAccProviders,
		CheckDestroy: testAccCheckPostgresqlRoleDestroy,
		Steps: []resource.TestStep{
			{
				Config: configCreate,
				Check: resource.ComposeTestCheckFunc(
					testAccCheckPostgresqlRoleExists("role_with_createrole_self_grant", nil, nil),
					testAccCheckPostgresqlRoleParameters("role_with_createrole_self_grant", map[string]string{"createrole_self_grant": "set, inherit"}),
				),
			},
			{
				Config: configUpdate,
				Check: resource.ComposeTestCheckFunc(
					testAccCheckPostgresqlRoleExists("role_with_createrole_self_grant", nil, nil),
					testAccCheckPostgresqlRoleParameters("role_with_createrole_self_grant", map[string]string{"createrole_self_grant": "set"}),
				),
			},
			{
				Config: configReset,
				Check: resource.ComposeTestCheckFunc(
					testAccCheckPostgresqlRoleExists("role_with_createrole_self_grant", nil, nil),
					testAccCheckPostgresqlRoleParameters("role_with_createrole_self_grant", map[string]string{}),
				),
			},
			// check parameters are reset by deleting parameters container
			{
				Config: configCreate,
				Check: resource.ComposeTestCheckFunc(
					testAccCheckPostgresqlRoleExists("role_with_createrole_self_grant", nil, nil),
					testAccCheckPostgresqlRoleParameters("role_with_createrole_self_grant", map[string]string{"createrole_self_grant": "set, inherit"}),
				),
			},
			{
				Config: configNoParams,
				Check: resource.ComposeTestCheckFunc(
					testAccCheckPostgresqlRoleExists("role_with_createrole_self_grant", nil, nil),
					testAccCheckPostgresqlRoleParameters("role_with_createrole_self_grant", map[string]string{}),
				),
			},
		},
	})
}

func TestAccPostgresqlRole_UnsupportedRoleParam(t *testing.T) {
	var configCreate = `
resource "postgresql_role" "role_with_unsupported_param_lol" {
  name = "role_with_unsupported_param_lol"
  parameters = {
    unsupported_param_lol = "N/A"
  }
}
`
	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccPreCheck(t)
			testCheckCompatibleVersion(t, featurePrivileges)
			testCheckCompatibleVersion(t, featureCreateRoleSelfGrant)
		},
		Providers:    testAccProviders,
		CheckDestroy: testAccCheckPostgresqlRoleDestroy,
		Steps: []resource.TestStep{
			{
				Config:      configCreate,
				ExpectError: regexp.MustCompile(`parameter unsupported_param_lol is not supported, only \[.+] parameters are supported yet`),
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
			testCheckCompatibleVersionRange(t, "<16.0.0") // PG 16: Only roles with the ADMIN option on role "<admin>" may grant this role.
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

func testAccCheckPostgresqlRoleParameters(roleName string, parameters map[string]string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		client := testAccProvider.Meta().(*Client)

		if parameters != nil {
			var errs []error
			for k, v := range parameters {
				if err := checkRoleParameter(client, roleName, k, v); err != nil {
					errs = append(errs, err)
				}
			}
			if len(errs) > 0 {
				return errors.Join(errs...)
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

func TestAccPostgresqlRole_DbOwner(t *testing.T) {
	config := getTestConfig(t)
	dsn := config.connStr("postgres")

	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccPreCheck(t)
			testCheckCompatibleVersion(t, featurePrivileges)
		},
		Providers: testAccProviders,
		Steps: []resource.TestStep{
			{
				Config: testAccPostgresqlRoleDbOwnerConfig,
				Check: resource.ComposeTestCheckFunc(
					testAccCheckPostgresqlRoleExists("owned_db_owner", nil, nil),
					resource.TestCheckResourceAttr("postgresql_database.owned_db", "owner", "owned_db_owner"),
					resource.TestCheckResourceAttr("postgresql_database.owned_db", "name", "owned_db"),

					func(state *terraform.State) error {
						connect, _ := config.NewClient("postgres").Connect()
						if connect.featureSupported(featureCreateRoleSelfGrant) {
							// in PG 16 all created roles have creator grant with admin option
							checkUserMembership(t, dsn, config.Username, "owned_db_owner", true)
						} else {
							// check if connected user does not have test_owner granted anymore.
							checkUserMembership(t, dsn, config.Username, "owned_db_owner", false)
						}
						return nil
					},
				),
			},
		},
	})
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

func checkRoleParameter(client *Client, roleName string, name string, value string) error {
	db, err := client.Connect()
	if err != nil {
		return err
	}

	expectedParameter := fmt.Sprintf("%s=%s", name, value)
	query := fmt.Sprintf(`SELECT 1 FROM pg_catalog.pg_roles WHERE rolname = $1 AND jsonb_path_exists(to_jsonb(rolconfig), '$[*] ? (@ like_regex "^%s$")')`, expectedParameter)
	result, err := db.Query(query, roleName)
	if err != nil {
		return err
	}
	if !result.Next() {
		return fmt.Errorf("role '%s' parameters are not set to expected value. expected '%v' but nothing found", roleName, expectedParameter)
	}
	return nil
}

var testAccPostgresqlRoleConfig = `
resource "postgresql_role" "myrole2" {
  name  = "myrole2"
  login = true
}

resource "postgresql_role" "role_with_pwd" {
  name     = "role_with_pwd"
  login    = true
  password = "mypass"
}

resource "postgresql_role" "role_with_pwd_encr" {
  name               = "role_with_pwd_encr"
  login              = true
  password           = "mypass"
  encrypted_password = true
}

resource "postgresql_role" "role_simple" {
  name = "role_simple"
}

resource "postgresql_role" "role_with_defaults" {
  name                                = "role_default"
  superuser                           = false
  create_database                     = false
  create_role                         = false
  inherit                             = false
  login                               = false
  replication                         = false
  bypass_row_level_security           = false
  connection_limit                    = -1
  encrypted_password                  = true
  password                            = ""
  skip_drop_role                      = false
  valid_until                         = "infinity"
  statement_timeout                   = 0
  idle_in_transaction_session_timeout = 0
  assume_role                         = ""
}

resource "postgresql_role" "role_with_create_database" {
  name            = "role_with_create_database"
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
  name        = "role_with_search_path"
  search_path = ["bar", "foo-with-hyphen"]
}
`

// based on comment for issue 407
// https://github.com/cyrilgdn/terraform-provider-postgresql/issues/407#issue-2127498162
var testAccPostgresqlRoleDbOwnerConfig = `
resource "postgresql_role" "owned_db_owner" {
  name     = "owned_db_owner"
  login    = true
  password = "veryS3cret"
}

resource "postgresql_database" "owned_db" {
  name              = "owned_db"
  owner             = postgresql_role.owned_db_owner.name
  lc_collate        = "en_US.utf8"
  allow_connections = true
}
`
