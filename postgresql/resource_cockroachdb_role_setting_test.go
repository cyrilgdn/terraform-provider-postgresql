package postgresql

import (
	"fmt"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
)

func testAccCheckCockroachDBRoleSettingDestroy(s *terraform.State) error {
	client := testAccProvider.Meta().(*Client)

	for _, rs := range s.RootModule().Resources {
		if rs.Type != "postgresql_crdb_role_setting" {
			continue
		}

		db, err := client.Connect()
		if err != nil {
			return err
		}

		parts := strings.SplitN(rs.Primary.ID, ":", 2)
		if len(parts) != 2 {
			return fmt.Errorf("invalid session setting ID %q", rs.Primary.ID)
		}
		roleName, settingName := parts[0], parts[1]

		_, found, err := readRoleSetting(db, roleName, settingName)
		if err != nil {
			return fmt.Errorf("error checking session setting %s: %w", rs.Primary.ID, err)
		}

		if found {
			return fmt.Errorf("session setting %s still exists after destroy", rs.Primary.ID)
		}
	}

	return nil
}

func testAccCheckCockroachDBRoleSettingExists(n string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[n]
		if !ok {
			return fmt.Errorf("Resource not found: %s", n)
		}

		if rs.Primary.ID == "" {
			return fmt.Errorf("No ID is set")
		}

		db, err := testAccProvider.Meta().(*Client).Connect()
		if err != nil {
			return err
		}

		roleName := rs.Primary.Attributes[RoleName]
		settingName := rs.Primary.Attributes[SettingName]

		_, found, err := readRoleSetting(db, roleName, settingName)
		if err != nil {
			return fmt.Errorf("error checking session setting: %w", err)
		}

		if !found {
			return fmt.Errorf("session setting not found")
		}

		return nil
	}
}

func TestAccCockroachDBRoleSetting_Basic(t *testing.T) {
	skipIfNotAcc(t)

	config := `
resource "postgresql_crdb_role_setting" "test" {
  role_name     = "ALL"
  setting_name  = "statement_timeout"
  setting_value = "1m"
}
`

	resource.Test(t, resource.TestCase{
		PreCheck:     func() { testAccPreCheck(t) },
		Providers:    testAccProviders,
		CheckDestroy: testAccCheckCockroachDBRoleSettingDestroy,
		Steps: []resource.TestStep{
			{
				Config: config,
				Check: resource.ComposeTestCheckFunc(
					testAccCheckCockroachDBRoleSettingExists("postgresql_crdb_role_setting.test"),
					resource.TestCheckResourceAttr(
						"postgresql_crdb_role_setting.test", "role_name", "ALL"),
					resource.TestCheckResourceAttr(
						"postgresql_crdb_role_setting.test", "setting_name", "statement_timeout"),
				),
			},
		},
	})
}

func TestRoleSettingValueSQL(t *testing.T) {
	cases := map[string]string{
		"100000":     "100000",
		"-1":         "-1",
		"0.5":        "0.5",
		"30s":        "'30s'",
		"true":       "'true'",
		"background": "'background'",
		"1e5":        "'1e5'",
		"1; DROP x":  "'1; DROP x'",
		"it's":       "'it''s'",
	}
	for in, want := range cases {
		if got := roleSettingValueSQL(in); got != want {
			t.Errorf("roleSettingValueSQL(%q) = %s, want %s", in, got, want)
		}
	}
}

// Integer settings reject quoted values ("requires an integer value").
func TestAccCockroachDBRoleSetting_IntegerValue(t *testing.T) {
	skipIfNotAcc(t)

	config := `
resource "postgresql_crdb_role_setting" "test" {
  role_name     = "ALL"
  setting_name  = "transaction_rows_read_err"
  setting_value = "100000"
}
`

	resource.Test(t, resource.TestCase{
		PreCheck:     func() { testAccPreCheck(t) },
		Providers:    testAccProviders,
		CheckDestroy: testAccCheckCockroachDBRoleSettingDestroy,
		Steps: []resource.TestStep{
			{
				Config: config,
				Check: resource.ComposeTestCheckFunc(
					testAccCheckCockroachDBRoleSettingExists("postgresql_crdb_role_setting.test"),
					resource.TestCheckResourceAttr(
						"postgresql_crdb_role_setting.test", "setting_value", "100000"),
				),
			},
		},
	})
}

func TestAccCockroachDBRoleSetting_Update(t *testing.T) {
	skipIfNotAcc(t)

	configInitial := `
resource "postgresql_crdb_role_setting" "test_update" {
  role_name     = "ALL"
  setting_name  = "statement_timeout"
  setting_value = "1m"
}
`

	configUpdated := `
resource "postgresql_crdb_role_setting" "test_update" {
  role_name     = "ALL"
  setting_name  = "statement_timeout"
  setting_value = "2m"
}
`

	resource.Test(t, resource.TestCase{
		PreCheck:     func() { testAccPreCheck(t) },
		Providers:    testAccProviders,
		CheckDestroy: testAccCheckCockroachDBRoleSettingDestroy,
		Steps: []resource.TestStep{
			{
				Config: configInitial,
				Check: resource.ComposeTestCheckFunc(
					testAccCheckCockroachDBRoleSettingExists("postgresql_crdb_role_setting.test_update"),
				),
			},
			{
				Config: configUpdated,
				Check: resource.ComposeTestCheckFunc(
					testAccCheckCockroachDBRoleSettingExists("postgresql_crdb_role_setting.test_update"),
				),
			},
		},
	})
}

func TestAccCockroachDBRoleSetting_MultipleSettings(t *testing.T) {
	skipIfNotAcc(t)

	config := `
resource "postgresql_crdb_role_setting" "timeout" {
  role_name     = "ALL"
  setting_name  = "statement_timeout"
  setting_value = "1m"
}

resource "postgresql_crdb_role_setting" "idle_timeout" {
  role_name     = "ALL"
  setting_name  = "idle_in_session_timeout"
  setting_value = "5m"
}
`

	resource.Test(t, resource.TestCase{
		PreCheck:     func() { testAccPreCheck(t) },
		Providers:    testAccProviders,
		CheckDestroy: testAccCheckCockroachDBRoleSettingDestroy,
		Steps: []resource.TestStep{
			{
				Config: config,
				Check: resource.ComposeTestCheckFunc(
					testAccCheckCockroachDBRoleSettingExists("postgresql_crdb_role_setting.timeout"),
					testAccCheckCockroachDBRoleSettingExists("postgresql_crdb_role_setting.idle_timeout"),
					resource.TestCheckResourceAttr(
						"postgresql_crdb_role_setting.timeout", "setting_name", "statement_timeout"),
					resource.TestCheckResourceAttr(
						"postgresql_crdb_role_setting.idle_timeout", "setting_name", "idle_in_session_timeout"),
				),
			},
		},
	})
}
