package postgresql

import (
	"database/sql"
	"fmt"
	"strconv"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
	"github.com/lib/pq"
)

func TestCreateGrantRoleQuery(t *testing.T) {
	var roleName = "foo"
	var grantRoleName = "bar"

	cases := []struct {
		resource map[string]any
		expected string
	}{
		{
			resource: map[string]any{
				"role":       roleName,
				"grant_role": grantRoleName,
			},
			expected: fmt.Sprintf("GRANT %s TO %s", pq.QuoteIdentifier(grantRoleName), pq.QuoteIdentifier(roleName)),
		},
		{
			resource: map[string]any{
				"role":              roleName,
				"grant_role":        grantRoleName,
				"with_admin_option": false,
			},
			expected: fmt.Sprintf("GRANT %s TO %s", pq.QuoteIdentifier(grantRoleName), pq.QuoteIdentifier(roleName)),
		},
		{
			resource: map[string]any{
				"role":              roleName,
				"grant_role":        grantRoleName,
				"with_admin_option": true,
			},
			expected: fmt.Sprintf("GRANT %s TO %s WITH ADMIN OPTION", pq.QuoteIdentifier(grantRoleName), pq.QuoteIdentifier(roleName)),
		},
		{
			resource: map[string]any{
				"role":                roleName,
				"grant_role":          grantRoleName,
				"with_inherit_option": false,
			},
			expected: fmt.Sprintf("GRANT %s TO %s WITH INHERIT FALSE", pq.QuoteIdentifier(grantRoleName), pq.QuoteIdentifier(roleName)),
		},
		{
			resource: map[string]any{
				"role":                roleName,
				"grant_role":          grantRoleName,
				"with_inherit_option": true,
			},
			expected: fmt.Sprintf("GRANT %s TO %s WITH INHERIT TRUE", pq.QuoteIdentifier(grantRoleName), pq.QuoteIdentifier(roleName)),
		},
		{
			resource: map[string]any{
				"role":            roleName,
				"grant_role":      grantRoleName,
				"with_set_option": false,
			},
			expected: fmt.Sprintf("GRANT %s TO %s WITH SET FALSE", pq.QuoteIdentifier(grantRoleName), pq.QuoteIdentifier(roleName)),
		},
		{
			resource: map[string]any{
				"role":            roleName,
				"grant_role":      grantRoleName,
				"with_set_option": true,
			},
			expected: fmt.Sprintf("GRANT %s TO %s WITH SET TRUE", pq.QuoteIdentifier(grantRoleName), pq.QuoteIdentifier(roleName)),
		},
		{
			resource: map[string]any{
				"role":                roleName,
				"grant_role":          grantRoleName,
				"with_admin_option":   true,
				"with_inherit_option": false,
				"with_set_option":     true,
			},
			expected: fmt.Sprintf(
				"GRANT %s TO %s WITH ADMIN OPTION, INHERIT FALSE, SET TRUE",
				pq.QuoteIdentifier(grantRoleName),
				pq.QuoteIdentifier(roleName),
			),
		},
	}

	for _, c := range cases {
		out := createGrantRoleQuery(schema.TestResourceDataRaw(t, resourcePostgreSQLGrantRole().Schema, c.resource))
		if out != c.expected {
			t.Fatalf("error matching output and expected: %#v vs %#v", out, c.expected)
		}
	}
}

func TestRevokeRoleQuery(t *testing.T) {
	var roleName = "foo"
	var grantRoleName = "bar"

	expected := fmt.Sprintf("REVOKE %s FROM %s", pq.QuoteIdentifier(grantRoleName), pq.QuoteIdentifier(roleName))

	cases := []struct {
		resource map[string]any
	}{
		{
			resource: map[string]any{
				"role":       roleName,
				"grant_role": grantRoleName,
			},
		},
		{
			resource: map[string]any{
				"role":              roleName,
				"grant_role":        grantRoleName,
				"with_admin_option": false,
			},
		},
		{
			resource: map[string]any{
				"role":              roleName,
				"grant_role":        grantRoleName,
				"with_admin_option": true,
			},
		},
		{
			resource: map[string]any{
				"role":                roleName,
				"grant_role":          grantRoleName,
				"with_inherit_option": false,
				"with_set_option":     true,
			},
		},
	}

	for _, c := range cases {
		out := createRevokeRoleQuery(schema.TestResourceDataRaw(t, resourcePostgreSQLGrantRole().Schema, c.resource))
		if out != expected {
			t.Fatalf("error matching output and expected: %#v vs %#v", out, expected)
		}
	}
}

func TestAccPostgresqlGrantRole(t *testing.T) {
	skipIfNotAcc(t)

	config := getTestConfig(t)
	dsn := config.connStr("postgres")

	dbSuffix, teardown := setupTestDatabase(t, false, true)
	defer teardown()

	_, roleName := getTestDBNames(dbSuffix)

	grantedRoleName := "foo"

	testAccPostgresqlGrantRoleResources := fmt.Sprintf(`
	resource postgresql_role "grant" {
		name = "%s"
	}
	resource postgresql_grant_role "grant_role" {
		role              = "%s"
		grant_role        = postgresql_role.grant.name
		with_admin_option = true
	}
	`, grantedRoleName, roleName)

	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccPreCheck(t)
			testCheckCompatibleVersion(t, featurePrivileges)
		},
		Providers: testAccProviders,
		Steps: []resource.TestStep{
			{
				Config: testAccPostgresqlGrantRoleResources,
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(
						"postgresql_grant_role.grant_role", "role", roleName),
					resource.TestCheckResourceAttr(
						"postgresql_grant_role.grant_role", "grant_role", grantedRoleName),
					resource.TestCheckResourceAttr(
						"postgresql_grant_role.grant_role", "with_admin_option", strconv.FormatBool(true)),
					checkGrantRole(t, dsn, roleName, grantedRoleName, true, nil, nil),
				),
			},
		},
	})
}

func TestAccPostgresqlGrantRoleInheritSet(t *testing.T) {
	skipIfNotAcc(t)

	config := getTestConfig(t)
	dsn := config.connStr("postgres")

	dbSuffix, teardown := setupTestDatabase(t, false, true)
	defer teardown()

	_, roleName := getTestDBNames(dbSuffix)

	grantedRoleName := "foo_inherit"

	inheritFalse := false
	setTrue := true

	testAccPostgresqlGrantRoleResources := fmt.Sprintf(`
	resource postgresql_role "grant" {
		name = "%s"
	}
	resource postgresql_grant_role "grant_role" {
		role                 = "%s"
		grant_role           = postgresql_role.grant.name
		with_inherit_option  = false
		with_set_option      = true
	}
	`, grantedRoleName, roleName)

	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccPreCheck(t)
			testCheckCompatibleVersion(t, featureGrantRoleMembershipOptions)
		},
		Providers: testAccProviders,
		Steps: []resource.TestStep{
			{
				Config: testAccPostgresqlGrantRoleResources,
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(
						"postgresql_grant_role.grant_role", "role", roleName),
					resource.TestCheckResourceAttr(
						"postgresql_grant_role.grant_role", "grant_role", grantedRoleName),
					resource.TestCheckResourceAttr(
						"postgresql_grant_role.grant_role", "with_inherit_option", strconv.FormatBool(false)),
					resource.TestCheckResourceAttr(
						"postgresql_grant_role.grant_role", "with_set_option", strconv.FormatBool(true)),
					checkGrantRole(t, dsn, roleName, grantedRoleName, false, &inheritFalse, &setTrue),
				),
			},
		},
	})
}

func checkGrantRole(t *testing.T, dsn, role string, grantRole string, withAdmin bool, withInherit, withSet *bool) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		db, err := sql.Open("postgres", dsn)
		if err != nil {
			t.Fatalf("could to create connection pool: %v", err)
		}
		defer closeDB(t, db)

		query := `
		SELECT 1
		FROM pg_auth_members
		WHERE pg_get_userbyid(member) = $1
		AND pg_get_userbyid(roleid) = $2
		AND admin_option = $3`
		args := []any{role, grantRole, withAdmin}

		if withInherit != nil {
			query += fmt.Sprintf(" AND inherit_option = $%d", len(args)+1)
			args = append(args, *withInherit)
		}
		if withSet != nil {
			query += fmt.Sprintf(" AND set_option = $%d", len(args)+1)
			args = append(args, *withSet)
		}

		var _rez int
		err = db.QueryRow(query, args...).Scan(&_rez)

		switch {
		case err == sql.ErrNoRows:
			return fmt.Errorf(
				"Role %s is not a member of %s with expected options",
				role, grantRole,
			)

		case err != nil:
			t.Fatalf("could not check granted role: %v", err)
		}

		return nil
	}
}
