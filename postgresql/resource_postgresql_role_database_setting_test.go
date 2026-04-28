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
			parts, err := splitIDComponents(encoded)
			if err != nil {
				t.Fatalf("unexpected error splitting valid ID %q: %s", encoded, err)
			}
			if len(parts) != 3 {
				t.Fatalf("split returned %d parts, want 3: %v", len(parts), parts)
			}
			if parts[0] != tc.role || parts[1] != tc.database || parts[2] != tc.parameter {
				t.Fatalf("round-trip mismatch: got %q/%q/%q, want %q/%q/%q",
					parts[0], parts[1], parts[2], tc.role, tc.database, tc.parameter)
			}
		})
	}

	invalidIDs := []string{
		`alice\dev:app:role`,
		`alice:app:role\`,
		`alice\a:app:role`,
	}
	for _, id := range invalidIDs {
		if _, err := splitIDComponents(id); err == nil {
			t.Fatalf("splitIDComponents(%q) should have failed on invalid escape", id)
		}
	}
}

// TestFindSetconfigValue exercises pure catalog parsing against the formats
// PostgreSQL produces in pg_db_role_setting.setconfig.
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
			name:      "list value with multiple identifiers",
			setconfig: []string{"search_path=app, public"},
			parameter: "search_path",
			want:      "app, public",
			wantFound: true,
		},
		{
			name:      "list value with single quoted identifier containing comma",
			setconfig: []string{`search_path="a,b"`},
			parameter: "search_path",
			want:      `"a,b"`,
			wantFound: true,
		},
		{
			name:      "list value with mixed quoted and unquoted identifiers",
			setconfig: []string{`search_path="a,b", public`},
			parameter: "search_path",
			want:      `"a,b", public`,
			wantFound: true,
		},
		{
			name: "multiple parameters, pick the requested one",
			setconfig: []string{
				"search_path=app, public",
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
			name:      "unwrapped scalar value containing literal quote and comma",
			setconfig: []string{`application_name=has"quote,comma`},
			parameter: "application_name",
			want:      `has"quote,comma`,
			wantFound: true,
		},
		{
			name:      "scalar value with surrounding double quotes preserved verbatim",
			setconfig: []string{`application_name="worker"`},
			parameter: "application_name",
			want:      `"worker"`,
			wantFound: true,
		},
		{
			name:      "scalar value with doubled double quotes preserved verbatim",
			setconfig: []string{`application_name="worker""1"`},
			parameter: "application_name",
			want:      `"worker""1"`,
			wantFound: true,
		},
		{
			name:      "empty list value represented as double quotes in catalog",
			setconfig: []string{`search_path=""`},
			parameter: "search_path",
			want:      `""`,
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

func TestFormatListSettingValue(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "single identifier",
			input: "public",
			want:  `"public"`,
		},
		{
			name:  "comma-separated identifiers",
			input: "app, public",
			want:  `"app", "public"`,
		},
		{
			name:  "quoted special identifier with dollar",
			input: `"$user", public`,
			want:  `"$user", "public"`,
		},
		{
			name:  "embedded comma in quoted identifier",
			input: `"a,b", public`,
			want:  `"a,b", "public"`,
		},
		{
			name:  "compact comma-separated without space",
			input: "app,public",
			want:  `"app", "public"`,
		},
		{
			name:  "single-quoted identifiers",
			input: "'app', 'public'",
			want:  `"app", "public"`,
		},
		{
			name:  "empty string returns empty literal",
			input: "",
			want:  "''",
		},
		{
			name:  "double-quoted empty string returns empty literal",
			input: `""`,
			want:  "''",
		},
		{
			name:  "single-quoted empty string returns empty literal",
			input: "''",
			want:  "''",
		},
		{
			name:  "empty quoted item in multi-item list",
			input: `"", public`,
			want:  `'', "public"`,
		},
		{
			name:  "single-quoted empty item in multi-item list",
			input: `'', public`,
			want:  `'', "public"`,
		},
		{
			name:  "multiple empty items in list",
			input: `"", ''`,
			want:  `'', ''`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := formatListSettingValue(tc.input)
			if got != tc.want {
				t.Fatalf("formatListSettingValue(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestSuppressSettingValueDiff(t *testing.T) {
	res := resourcePostgreSQLRoleDatabaseSetting()

	cases := []struct {
		name      string
		parameter string
		old       string
		new       string
		want      bool
	}{
		{
			name:      "identical strings",
			parameter: "search_path",
			old:       "app, public",
			new:       "app, public",
			want:      true,
		},
		{
			name:      "list spacing differences suppressed",
			parameter: "search_path",
			old:       "app, public",
			new:       "app,public",
			want:      true,
		},
		{
			name:      "list single quotes vs catalog unquoted suppressed",
			parameter: "search_path",
			old:       "app, public",
			new:       "'app', 'public'",
			want:      true,
		},
		{
			name:      "list double quotes vs catalog unquoted suppressed",
			parameter: "search_path",
			old:       "app, public",
			new:       `"app", "public"`,
			want:      true,
		},
		{
			name:      "list unquoted case differences suppressed due to pg lower folding",
			parameter: "search_path",
			old:       "app, public",
			new:       "APP, public",
			want:      true,
		},
		{
			name:      "list uppercase quoted vs unquoted same case not suppressed due to pg lower folding of unquoted",
			parameter: "search_path",
			old:       "APP",
			new:       `"APP"`,
			want:      false,
		},
		{
			name:      "list uppercase quoted vs quoted suppressed",
			parameter: "search_path",
			old:       `"APP"`,
			new:       `"APP"`,
			want:      true,
		},
		{
			name:      "list lowercase quoted vs unquoted suppressed",
			parameter: "search_path",
			old:       "app",
			new:       `"app"`,
			want:      true,
		},
		{
			name:      "list with empty item diff suppressed against catalog",
			parameter: "search_path",
			old:       `"", public`,
			new:       `'', public`,
			want:      true,
		},
		{
			name:      "empty list empty string vs catalog quotes suppressed",
			parameter: "search_path",
			old:       `""`,
			new:       "",
			want:      true,
		},
		{
			name:      "empty list reverse suppressed",
			parameter: "search_path",
			old:       "",
			new:       `""`,
			want:      true,
		},
		{
			name:      "single identifier with comma vs two unquoted schemas not suppressed",
			parameter: "search_path",
			old:       `"a,b"`,
			new:       "a, b",
			want:      false,
		},
		{
			name:      "different list items not suppressed",
			parameter: "search_path",
			old:       "app, public",
			new:       "other, public",
			want:      false,
		},
		{
			name:      "scalar strings not suppressed when different",
			parameter: "application_name",
			old:       "worker1",
			new:       "worker2",
			want:      false,
		},
		{
			name:      "scalar strings suppressed when identical",
			parameter: "application_name",
			old:       `"worker"`,
			new:       `"worker"`,
			want:      true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := res.TestResourceData()
			_ = d.Set(roleDatabaseSettingParameterAttr, tc.parameter)
			got := suppressSettingValueDiff("value", tc.old, tc.new, d)
			if got != tc.want {
				t.Fatalf("suppressSettingValueDiff(%q, %q) for param %q = %v, want %v",
					tc.old, tc.new, tc.parameter, got, tc.want)
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
				Check: resource.ComposeTestCheckFunc(
					testAccCheckPostgresqlRoleDatabaseSettingValue(
						"rds_test_owner@example.com", "rds_test_db", "search_path", "public",
					),
					testAccCheckEffectiveSessionSearchPath(
						"rds_test_owner@example.com", "rds_test_db", "testpassword123", "public",
					),
				),
			},
			{
				Config: testAccPostgresqlRoleDatabaseSettingSearchPathConfig("app, public"),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckPostgresqlRoleDatabaseSettingValue(
						"rds_test_owner@example.com", "rds_test_db", "search_path", "app, public",
					),
					testAccCheckEffectiveSessionSearchPath(
						"rds_test_owner@example.com", "rds_test_db", "testpassword123", "app, public",
					),
				),
			},
		},
	})
}

func TestAccPostgresqlRoleDatabaseSetting_EmptyListValue(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccPreCheck(t)
			testSuperuserPreCheck(t)
		},
		Providers:    testAccProviders,
		CheckDestroy: testAccCheckPostgresqlRoleDatabaseSettingDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccPostgresqlRoleDatabaseSettingSearchPathConfig(""),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckPostgresqlRoleDatabaseSettingValue(
						"rds_test_owner@example.com", "rds_test_db", "search_path", `""`,
					),
					testAccCheckEffectiveSessionSearchPath(
						"rds_test_owner@example.com", "rds_test_db", "testpassword123", `""`,
					),
				),
			},
			{
				Config:   testAccPostgresqlRoleDatabaseSettingSearchPathConfig(""),
				PlanOnly: true,
			},
			{
				Config: testAccPostgresqlRoleDatabaseSettingSearchPathConfig(`"", public`),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckPostgresqlRoleDatabaseSettingValue(
						"rds_test_owner@example.com", "rds_test_db", "search_path", `"", public`,
					),
				),
			},
			{
				Config:   testAccPostgresqlRoleDatabaseSettingSearchPathConfig(`"", public`),
				PlanOnly: true,
			},
		},
	})
}

func TestAccPostgresqlRoleDatabaseSetting_ScalarQuotedValue(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccPreCheck(t)
			testSuperuserPreCheck(t)
		},
		Providers:    testAccProviders,
		CheckDestroy: testAccCheckPostgresqlRoleDatabaseSettingDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccPostgresqlRoleDatabaseSettingApplicationNameConfig(`"worker"`),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckPostgresqlRoleDatabaseSettingValue(
						"rds_test_owner@example.com", "rds_test_db", "application_name", `"worker"`,
					),
					resource.TestCheckResourceAttr(
						"postgresql_role_database_setting.app_name", "value", `"worker"`,
					),
				),
			},
			{
				Config:   testAccPostgresqlRoleDatabaseSettingApplicationNameConfig(`"worker"`),
				PlanOnly: true,
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
			// Step 1: Create two settings on the same (role, database) pair
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
			// Step 2: Remove search_path setting while retaining role setting, role, and database
			{
				Config: testAccPostgresqlRoleDatabaseSettingMultiConfigOnlyRole,
				Check: resource.ComposeTestCheckFunc(
					testAccCheckPostgresqlRoleDatabaseSettingValue(
						"rds_test_owner@example.com", "rds_test_db", "role", "rds_test_owner",
					),
					testAccCheckPostgresqlRoleDatabaseSettingAbsent(
						"rds_test_owner@example.com", "rds_test_db", "search_path",
					),
					testAccCheckRoleAndDatabaseExist(
						"rds_test_owner@example.com", "rds_test_db",
					),
				),
			},
			// Step 3: Remove the last setting (role) while retaining role and database
			{
				Config: testAccPostgresqlRoleDatabaseSettingMultiConfigNoSettings,
				Check: resource.ComposeTestCheckFunc(
					testAccCheckPostgresqlRoleDatabaseSettingAbsent(
						"rds_test_owner@example.com", "rds_test_db", "role",
					),
					testAccCheckPostgresqlRoleDatabaseSettingAbsent(
						"rds_test_owner@example.com", "rds_test_db", "search_path",
					),
					testAccCheckRoleAndDatabaseExist(
						"rds_test_owner@example.com", "rds_test_db",
					),
				),
			},
		},
	})
}

// TestAccPostgresqlRoleDatabaseSetting_EmbeddedQuoteValue exercises the
// catalog round-trip for a search_path value containing an embedded double
// quote.
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
			{
				Config:   testAccPostgresqlRoleDatabaseSettingSearchPathConfig(`"a,b", public`),
				PlanOnly: true,
			},
		},
	})
}

// TestAccPostgresqlRoleDatabaseSetting_SingleQuotedIdentifierWithComma verifies
// that a single identifier containing a comma is preserved verbatim with quotes.
func TestAccPostgresqlRoleDatabaseSetting_SingleQuotedIdentifierWithComma(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccPreCheck(t)
			testSuperuserPreCheck(t)
		},
		Providers:    testAccProviders,
		CheckDestroy: testAccCheckPostgresqlRoleDatabaseSettingDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccPostgresqlRoleDatabaseSettingSearchPathConfig(`"a,b"`),
				Check: testAccCheckPostgresqlRoleDatabaseSettingValue(
					"rds_test_owner@example.com", "rds_test_db", "search_path", `"a,b"`,
				),
			},
			{
				Config:   testAccPostgresqlRoleDatabaseSettingSearchPathConfig(`"a,b"`),
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

func testAccCheckPostgresqlRoleDatabaseSettingAbsent(role, database, parameter string) resource.TestCheckFunc {
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
		switch {
		case err == sql.ErrNoRows:
			return nil
		case err != nil:
			return fmt.Errorf("error reading pg_db_role_setting: %w", err)
		}

		if _, found := findSetconfigValue(setconfig, parameter); found {
			return fmt.Errorf(
				"expected parameter %q to be absent for (%s, %s), but found in setconfig %v",
				parameter, role, database, setconfig,
			)
		}
		return nil
	}
}

func testAccCheckRoleAndDatabaseExist(role, database string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		client := testAccProvider.Meta().(*Client)
		db, err := client.Connect()
		if err != nil {
			return err
		}
		ok, err := roleAndDatabaseExist(db, role, database)
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("expected role %q and database %q to still exist, but they were dropped", role, database)
		}
		return nil
	}
}

func testAccCheckEffectiveSessionSearchPath(role, database, password, expectedSearchPath string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		client := testAccProvider.Meta().(*Client)
		connStr := fmt.Sprintf(
			"host=%s port=%d dbname=%s user=%s password=%s sslmode=%s",
			client.config.Host, client.config.Port, database, role, password, client.config.SSLMode,
		)
		db, err := sql.Open("postgres", connStr)
		if err != nil {
			return fmt.Errorf("could not connect as %q to %q: %w", role, database, err)
		}
		defer db.Close()

		var sp string
		if err := db.QueryRow("SHOW search_path").Scan(&sp); err != nil {
			return fmt.Errorf("could not query SHOW search_path as %q in %q: %w", role, database, err)
		}
		if sp != expectedSearchPath {
			return fmt.Errorf("effective session search_path is %q, expected %q", sp, expectedSearchPath)
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

		parts, err := splitIDComponents(rs.Primary.ID)
		if err != nil || len(parts) != 3 {
			return fmt.Errorf("malformed resource ID %q: %v", rs.Primary.ID, err)
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
  name     = "rds_test_owner@example.com"
  login    = true
  password = "testpassword123"
  inherit  = true
  roles    = [postgresql_role.owner.name]
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

func testAccPostgresqlRoleDatabaseSettingApplicationNameConfig(appName string) string {
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

resource "postgresql_role_database_setting" "app_name" {
  role      = postgresql_role.user.name
  database  = postgresql_database.db.name
  parameter = "application_name"
  value     = %q
}
`, appName)
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

const testAccPostgresqlRoleDatabaseSettingMultiConfigOnlyRole = `
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

const testAccPostgresqlRoleDatabaseSettingMultiConfigNoSettings = `
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
`

func TestAccPostgresqlRoleDatabaseSetting_StatementTimeout(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccPreCheck(t)
			testSuperuserPreCheck(t)
		},
		Providers:    testAccProviders,
		CheckDestroy: testAccCheckPostgresqlRoleDatabaseSettingDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccPostgresqlRoleDatabaseSettingParameterConfig("statement_timeout", "5min"),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckPostgresqlRoleDatabaseSettingValue(
						"rds_test_owner@example.com", "rds_test_db", "statement_timeout", "5min",
					),
					resource.TestCheckResourceAttr(
						"postgresql_role_database_setting.setting", "value", "5min",
					),
				),
			},
			{
				Config:   testAccPostgresqlRoleDatabaseSettingParameterConfig("statement_timeout", "5min"),
				PlanOnly: true,
			},
		},
	})
}

func testAccPostgresqlRoleDatabaseSettingParameterConfig(param, val string) string {
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

resource "postgresql_role_database_setting" "setting" {
  role      = postgresql_role.user.name
  database  = postgresql_database.db.name
  parameter = %q
  value     = %q
}
`, param, val)
}

func TestAccPostgresqlRoleDatabaseSetting_ScalarBackslash(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccPreCheck(t)
			testSuperuserPreCheck(t)
		},
		Providers:    testAccProviders,
		CheckDestroy: testAccCheckPostgresqlRoleDatabaseSettingDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccPostgresqlRoleDatabaseSettingApplicationNameConfig(`worker\\node1`),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckPostgresqlRoleDatabaseSettingValue(
						"rds_test_owner@example.com", "rds_test_db", "application_name", `worker\\node1`,
					),
					resource.TestCheckResourceAttr(
						"postgresql_role_database_setting.app_name", "value", `worker\\node1`,
					),
				),
			},
			{
				Config:   testAccPostgresqlRoleDatabaseSettingApplicationNameConfig(`worker\\node1`),
				PlanOnly: true,
			},
		},
	})
}

func TestAccPostgresqlRoleDatabaseSetting_ParameterCaseNormalization(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccPreCheck(t)
			testSuperuserPreCheck(t)
		},
		Providers:    testAccProviders,
		CheckDestroy: testAccCheckPostgresqlRoleDatabaseSettingDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccPostgresqlRoleDatabaseSettingParameterConfig("SEARCH_PATH", "app, public"),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(
						"postgresql_role_database_setting.setting", "parameter", "SEARCH_PATH",
					),
					testAccCheckPostgresqlRoleDatabaseSettingValue(
						"rds_test_owner@example.com", "rds_test_db", "search_path", "app, public",
					),
				),
			},
			{
				Config:   testAccPostgresqlRoleDatabaseSettingParameterConfig("search_path", "app, public"),
				PlanOnly: true,
			},
		},
	})
}
