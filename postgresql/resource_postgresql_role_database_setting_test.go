package postgresql

import (
	"database/sql"
	"fmt"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
	"github.com/lib/pq"
)

// TestRoleDatabaseSettingIDRoundTrip checks that the composite resource ID
// encodes and decodes losslessly for identifiers that may legally contain
// ':' (PostgreSQL allows it in quoted identifiers) and '\'.
func TestRoleDatabaseSettingIDRoundTrip(t *testing.T) {
	cases := []struct {
		name                      string
		role, database, parameter string
		wantEncoded               string
	}{
		{
			name:        "no special chars",
			role:        "alice@example.com",
			database:    "app_db",
			parameter:   "role",
			wantEncoded: "alice@example.com:app_db:role",
		},
		{
			name:        "colon in database",
			role:        "alice",
			database:    "app:blue",
			parameter:   "role",
			wantEncoded: `alice:app\:blue:role`,
		},
		{
			name:        "colon in role",
			role:        "alice:dev",
			database:    "app",
			parameter:   "role",
			wantEncoded: `alice\:dev:app:role`,
		},
		{
			name:        "colons in role and database",
			role:        "alice:dev",
			database:    "app:blue",
			parameter:   "role",
			wantEncoded: `alice\:dev:app\:blue:role`,
		},
		{
			name:        "backslash in role",
			role:        `weird\name`,
			database:    "app_db",
			parameter:   "role",
			wantEncoded: `weird\\name:app_db:role`,
		},
		{
			name:        "backslash and colon mixed",
			role:        `with:colon\and\backslash`,
			database:    "app",
			parameter:   "role",
			wantEncoded: `with\:colon\\and\\backslash:app:role`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			encoded := strings.Join([]string{
				escapeIDComponent(tc.role),
				escapeIDComponent(tc.database),
				escapeIDComponent(tc.parameter),
			}, ":")
			if encoded != tc.wantEncoded {
				t.Fatalf("encoded = %q, want %q", encoded, tc.wantEncoded)
			}
			parts := splitIDComponents(encoded)
			if len(parts) != 3 {
				t.Fatalf("split returned %d parts, want 3: %v", len(parts), parts)
			}
			if parts[0] != tc.role || parts[1] != tc.database || parts[2] != tc.parameter {
				t.Fatalf("round-trip mismatch: got %q/%q/%q, want %q/%q/%q",
					parts[0], parts[1], parts[2], tc.role, tc.database, tc.parameter)
			}
		})
	}
}

// TestFindSetconfigValue exercises the pure parser against the formats
// PostgreSQL actually produces in pg_db_role_setting.setconfig (verified
// empirically on PG 16). It runs without a live database.
func TestFindSetconfigValue(t *testing.T) {
	cases := []struct {
		name      string
		setconfig []string
		parameter string
		want      string
		wantFound bool
	}{
		{
			name:      "unwrapped plain identifier",
			setconfig: []string{"role=app_db_owner"},
			parameter: "role",
			want:      "app_db_owner",
			wantFound: true,
		},
		{
			name:      "wrapped value with comma and space",
			setconfig: []string{`search_path="app, public"`},
			parameter: "search_path",
			want:      "app, public",
			wantFound: true,
		},
		{
			name:      "wrapped value with embedded double quote (doubled inside)",
			setconfig: []string{`search_path="""a,b"", public"`},
			parameter: "search_path",
			want:      `"a,b", public`,
			wantFound: true,
		},
		{
			name: "multiple parameters, pick the requested one",
			setconfig: []string{
				`search_path="app, public"`,
				"role=app_db_owner",
			},
			parameter: "role",
			want:      "app_db_owner",
			wantFound: true,
		},
		{
			name:      "case-insensitive parameter match",
			setconfig: []string{"role=app_db_owner"},
			parameter: "ROLE",
			want:      "app_db_owner",
			wantFound: true,
		},
		{
			name:      "parameter not present",
			setconfig: []string{"role=app_db_owner"},
			parameter: "search_path",
			wantFound: false,
		},
		{
			name:      "unwrapped value containing literal quote and comma (postgres leaves untouched)",
			setconfig: []string{`application_name=has"quote,comma`},
			parameter: "application_name",
			want:      `has"quote,comma`,
			wantFound: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, found := findSetconfigValue(tc.setconfig, tc.parameter)
			if found != tc.wantFound {
				t.Fatalf("found = %v, want %v", found, tc.wantFound)
			}
			if got != tc.want {
				t.Fatalf("value = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestAccPostgresqlRoleDatabaseSetting_Basic(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccPreCheck(t)
			testSuperuserPreCheck(t)
		},
		Providers:    testAccProviders,
		CheckDestroy: testAccCheckPostgresqlRoleDatabaseSettingDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccPostgresqlRoleDatabaseSettingBasicConfig,
				Check: resource.ComposeTestCheckFunc(
					testAccCheckPostgresqlRoleDatabaseSettingValue(
						"rds_test_owner@example.com", "rds_test_db", "role", "rds_test_owner",
					),
					resource.TestCheckResourceAttr(
						"postgresql_role_database_setting.assume", "value", "rds_test_owner",
					),
					resource.TestCheckResourceAttr(
						"postgresql_role_database_setting.assume", "id",
						"rds_test_owner@example.com:rds_test_db:role",
					),
				),
			},
		},
	})
}

func TestAccPostgresqlRoleDatabaseSetting_UpdateValue(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccPreCheck(t)
			testSuperuserPreCheck(t)
		},
		Providers:    testAccProviders,
		CheckDestroy: testAccCheckPostgresqlRoleDatabaseSettingDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccPostgresqlRoleDatabaseSettingSearchPathConfig("public"),
				Check: testAccCheckPostgresqlRoleDatabaseSettingValue(
					"rds_test_owner@example.com", "rds_test_db", "search_path", "public",
				),
			},
			{
				Config: testAccPostgresqlRoleDatabaseSettingSearchPathConfig("app, public"),
				Check: testAccCheckPostgresqlRoleDatabaseSettingValue(
					"rds_test_owner@example.com", "rds_test_db", "search_path", "app, public",
				),
			},
		},
	})
}

func TestAccPostgresqlRoleDatabaseSetting_MultipleParameters(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccPreCheck(t)
			testSuperuserPreCheck(t)
		},
		Providers:    testAccProviders,
		CheckDestroy: testAccCheckPostgresqlRoleDatabaseSettingDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccPostgresqlRoleDatabaseSettingMultiConfig,
				Check: resource.ComposeTestCheckFunc(
					testAccCheckPostgresqlRoleDatabaseSettingValue(
						"rds_test_owner@example.com", "rds_test_db", "role", "rds_test_owner",
					),
					testAccCheckPostgresqlRoleDatabaseSettingValue(
						"rds_test_owner@example.com", "rds_test_db", "search_path", "shared, app, public",
					),
				),
			},
		},
	})
}

// TestAccPostgresqlRoleDatabaseSetting_EmbeddedQuoteValue exercises the
// catalog round-trip for a search_path value containing an embedded double
// quote. PostgreSQL stores this in pg_db_role_setting using the wrapped form
// with doubled inner quotes (`search_path="""a,b"", public"`). The Read path
// must decode `""` → `"`, otherwise terraform plan reports a permanent
// false-positive drift on every refresh.
func TestAccPostgresqlRoleDatabaseSetting_EmbeddedQuoteValue(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccPreCheck(t)
			testSuperuserPreCheck(t)
		},
		Providers:    testAccProviders,
		CheckDestroy: testAccCheckPostgresqlRoleDatabaseSettingDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccPostgresqlRoleDatabaseSettingSearchPathConfig(`"a,b", public`),
				Check: testAccCheckPostgresqlRoleDatabaseSettingValue(
					"rds_test_owner@example.com", "rds_test_db", "search_path", `"a,b", public`,
				),
			},
			// A second step with the same config asserts no drift: if Read
			// returned a mangled value, terraform would propose a re-apply
			// here and the test would fail.
			{
				Config:   testAccPostgresqlRoleDatabaseSettingSearchPathConfig(`"a,b", public`),
				PlanOnly: true,
			},
		},
	})
}

// TestAccPostgresqlRoleDatabaseSetting_ColonInIdentifier verifies the
// resource handles role/database names containing ':' end-to-end: apply,
// state ID encoding, and import round-trip via ImportStateVerify.
func TestAccPostgresqlRoleDatabaseSetting_ColonInIdentifier(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccPreCheck(t)
			testSuperuserPreCheck(t)
		},
		Providers:    testAccProviders,
		CheckDestroy: testAccCheckPostgresqlRoleDatabaseSettingDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccPostgresqlRoleDatabaseSettingColonConfig,
				Check: resource.ComposeTestCheckFunc(
					testAccCheckPostgresqlRoleDatabaseSettingValue(
						"alice:dev", "rds_app:blue_db", "role", "rds_test_owner",
					),
					resource.TestCheckResourceAttr(
						"postgresql_role_database_setting.assume", "id",
						`alice\:dev:rds_app\:blue_db:role`,
					),
				),
			},
			{
				ResourceName:      "postgresql_role_database_setting.assume",
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

func TestAccPostgresqlRoleDatabaseSetting_Import(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccPreCheck(t)
			testSuperuserPreCheck(t)
		},
		Providers:    testAccProviders,
		CheckDestroy: testAccCheckPostgresqlRoleDatabaseSettingDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccPostgresqlRoleDatabaseSettingBasicConfig,
			},
			{
				ResourceName:      "postgresql_role_database_setting.assume",
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

func testAccCheckPostgresqlRoleDatabaseSettingValue(role, database, parameter, expected string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		client := testAccProvider.Meta().(*Client)
		txn, err := startTransaction(client, "")
		if err != nil {
			return err
		}
		defer deferredRollback(txn)

		var setconfig []string
		err = txn.QueryRow(`
SELECT s.setconfig
FROM pg_db_role_setting s
JOIN pg_roles r ON r.oid = s.setrole
JOIN pg_database d ON d.oid = s.setdatabase
WHERE r.rolname = $1 AND d.datname = $2`, role, database).Scan(pq.Array(&setconfig))
		if err == sql.ErrNoRows {
			return fmt.Errorf("no pg_db_role_setting row for role %q in database %q", role, database)
		}
		if err != nil {
			return fmt.Errorf("error reading pg_db_role_setting: %w", err)
		}

		got, found := findSetconfigValue(setconfig, parameter)
		if !found {
			return fmt.Errorf("parameter %q not found in setconfig %v for (%s, %s)", parameter, setconfig, role, database)
		}
		if got != expected {
			return fmt.Errorf("parameter %q for (%s, %s) = %q, want %q", parameter, role, database, got, expected)
		}
		return nil
	}
}

func testAccCheckPostgresqlRoleDatabaseSettingDestroy(s *terraform.State) error {
	client := testAccProvider.Meta().(*Client)

	for _, rs := range s.RootModule().Resources {
		if rs.Type != "postgresql_role_database_setting" {
			continue
		}

		txn, err := startTransaction(client, "")
		if err != nil {
			return err
		}
		defer deferredRollback(txn)

		parts := splitIDComponents(rs.Primary.ID)
		if len(parts) != 3 {
			return fmt.Errorf("malformed resource ID %q", rs.Primary.ID)
		}
		role, database, parameter := parts[0], parts[1], parts[2]

		var setconfig []string
		err = txn.QueryRow(`
SELECT s.setconfig
FROM pg_db_role_setting s
JOIN pg_roles r ON r.oid = s.setrole
JOIN pg_database d ON d.oid = s.setdatabase
WHERE r.rolname = $1 AND d.datname = $2`, role, database).Scan(pq.Array(&setconfig))
		switch {
		case err == sql.ErrNoRows:
			// Either the row was removed entirely (RESET of last param) or
			// the role/database itself was dropped — either way the resource
			// is gone.
			continue
		case err != nil:
			return fmt.Errorf("error reading pg_db_role_setting: %w", err)
		}

		if _, found := findSetconfigValue(setconfig, parameter); found {
			return fmt.Errorf(
				"role-database setting %s for (%s, %s) still exists after destroy",
				parameter, role, database,
			)
		}
	}
	return nil
}

const testAccPostgresqlRoleDatabaseSettingBasicConfig = `
resource "postgresql_role" "owner" {
  name  = "rds_test_owner"
  login = false
}

resource "postgresql_role" "user" {
  name    = "rds_test_owner@example.com"
  login   = true
  inherit = true
  roles   = [postgresql_role.owner.name]
}

resource "postgresql_database" "db" {
  name              = "rds_test_db"
  owner             = postgresql_role.owner.name
  allow_connections = true
}

resource "postgresql_role_database_setting" "assume" {
  role      = postgresql_role.user.name
  database  = postgresql_database.db.name
  parameter = "role"
  value     = postgresql_role.owner.name
}
`

func testAccPostgresqlRoleDatabaseSettingSearchPathConfig(searchPath string) string {
	return fmt.Sprintf(`
resource "postgresql_role" "owner" {
  name  = "rds_test_owner"
  login = false
}

resource "postgresql_role" "user" {
  name    = "rds_test_owner@example.com"
  login   = true
  inherit = true
  roles   = [postgresql_role.owner.name]
}

resource "postgresql_database" "db" {
  name              = "rds_test_db"
  owner             = postgresql_role.owner.name
  allow_connections = true
}

resource "postgresql_role_database_setting" "search_path" {
  role      = postgresql_role.user.name
  database  = postgresql_database.db.name
  parameter = "search_path"
  value     = %q
}
`, searchPath)
}

const testAccPostgresqlRoleDatabaseSettingColonConfig = `
resource "postgresql_role" "owner" {
  name  = "rds_test_owner"
  login = false
}

resource "postgresql_role" "user" {
  name    = "alice:dev"
  login   = true
  inherit = true
  roles   = [postgresql_role.owner.name]
}

resource "postgresql_database" "db" {
  name              = "rds_app:blue_db"
  owner             = postgresql_role.owner.name
  allow_connections = true
}

resource "postgresql_role_database_setting" "assume" {
  role      = postgresql_role.user.name
  database  = postgresql_database.db.name
  parameter = "role"
  value     = postgresql_role.owner.name
}
`

const testAccPostgresqlRoleDatabaseSettingMultiConfig = `
resource "postgresql_role" "owner" {
  name  = "rds_test_owner"
  login = false
}

resource "postgresql_role" "user" {
  name    = "rds_test_owner@example.com"
  login   = true
  inherit = true
  roles   = [postgresql_role.owner.name]
}

resource "postgresql_database" "db" {
  name              = "rds_test_db"
  owner             = postgresql_role.owner.name
  allow_connections = true
}

resource "postgresql_role_database_setting" "assume" {
  role      = postgresql_role.user.name
  database  = postgresql_database.db.name
  parameter = "role"
  value     = postgresql_role.owner.name
}

resource "postgresql_role_database_setting" "search_path" {
  role      = postgresql_role.user.name
  database  = postgresql_database.db.name
  parameter = "search_path"
  value     = "shared, app, public"
}
`
