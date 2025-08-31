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
		resource map[string]interface{}
		expected string
	}{
		{
			resource: map[string]interface{}{
				"role":       roleName,
				"grant_role": grantRoleName,
			},
			expected: fmt.Sprintf("GRANT %s TO %s", pq.QuoteIdentifier(grantRoleName), pq.QuoteIdentifier(roleName)),
		},
		{
			resource: map[string]interface{}{
				"role":              roleName,
				"grant_role":        grantRoleName,
				"with_admin_option": false,
			},
			expected: fmt.Sprintf("GRANT %s TO %s", pq.QuoteIdentifier(grantRoleName), pq.QuoteIdentifier(roleName)),
		},
		{
			resource: map[string]interface{}{
				"role":              roleName,
				"grant_role":        grantRoleName,
				"with_admin_option": true,
			},
			expected: fmt.Sprintf("GRANT %s TO %s WITH ADMIN OPTION", pq.QuoteIdentifier(grantRoleName), pq.QuoteIdentifier(roleName)),
		},
	}

	for _, c := range cases {
		out := createGrantRoleQuery(schema.TestResourceDataRaw(t, resourcePostgreSQLGrantRole().Schema, c.resource))
		if out != c.expected {
			t.Fatalf("Error matching output and expected: %#v vs %#v", out, c.expected)
		}
	}
}

func TestRevokeRoleQuery(t *testing.T) {
	var roleName = "foo"
	var grantRoleName = "bar"

	expected := fmt.Sprintf("REVOKE %s FROM %s", pq.QuoteIdentifier(grantRoleName), pq.QuoteIdentifier(roleName))

	cases := []struct {
		resource map[string]interface{}
	}{
		{
			resource: map[string]interface{}{
				"role":       roleName,
				"grant_role": grantRoleName,
			},
		},
		{
			resource: map[string]interface{}{
				"role":              roleName,
				"grant_role":        grantRoleName,
				"with_admin_option": false,
			},
		},
		{
			resource: map[string]interface{}{
				"role":              roleName,
				"grant_role":        grantRoleName,
				"with_admin_option": true,
			},
		},
	}

	for _, c := range cases {
		out := createRevokeRoleQuery(schema.TestResourceDataRaw(t, resourcePostgreSQLGrantRole().Schema, c.resource))
		if out != expected {
			t.Fatalf("Error matching output and expected: %#v vs %#v", out, expected)
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
					checkGrantRole(t, dsn, roleName, grantedRoleName, true),
				),
			},
		},
	})
}

func checkGrantRole(t *testing.T, dsn, role string, grantRole string, withAdmin bool) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		db, err := sql.Open("postgres", dsn)
		if err != nil {
			t.Fatalf("could to create connection pool: %v", err)
		}
		deferDBClose(t, db)

		var _rez int
		err = db.QueryRow(`
		SELECT 1
		FROM pg_auth_members
		WHERE pg_get_userbyid(member) = $1
		AND pg_get_userbyid(roleid) = $2
		AND admin_option = $3;
		`, role, grantRole, withAdmin).Scan(&_rez)

		switch {
		case err == sql.ErrNoRows:
			return fmt.Errorf(
				"Role %s is not a member of %s",
				role, grantRole,
			)

		case err != nil:
			t.Fatalf("could not check granted role: %v", err)
		}

		return nil
	}
}
