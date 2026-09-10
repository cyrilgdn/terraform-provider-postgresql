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
					checkGrantRole(t, dsn, roleName, grantedRoleName, true),
				),
			},
		},
	})
}

// Since PostgreSQL 16 the same membership can be held once per grantor, each with
// its own admin_option. Only the grants this connection could have made are ours to
// manage: REVOKE removes just those. A grant made by a role we are not a member of
// must not be reported as this resource's state, or its admin_option is attributed to
// us and, because the attribute forces a new resource, the membership is revoked and
// re-granted on every apply without ever converging.
func TestAccPostgresqlGrantRoleForeignGrantor(t *testing.T) {
	skipIfNotAcc(t)

	config := getTestConfig(t)
	dsn := config.connStr("postgres")

	memberName := "test_foreign_grantor_member"
	grantedName := "test_foreign_grantor_granted"
	otherAdminName := "test_foreign_grantor_admin"

	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccPreCheck(t)
			testCheckCompatibleVersion(t, featureRoleMembershipGrantor)

			for _, role := range []string{memberName, grantedName, otherAdminName} {
				dbExecute(t, dsn, fmt.Sprintf("DROP ROLE IF EXISTS %s", role))
				dbExecute(t, dsn, fmt.Sprintf("CREATE ROLE %s", role))
			}

			// Give the competing grantor its own admin option, then have it grant the
			// membership. Ordered first so an unfixed read, which takes an arbitrary
			// row, sees this one.
			dbExecute(t, dsn, fmt.Sprintf("GRANT %s TO %s WITH ADMIN OPTION", grantedName, otherAdminName))
			// A CREATEROLE non-superuser is granted the roles it creates with ADMIN but
			// not SET, so ask for SET explicitly before stepping into the grantor.
			dbExecute(t, dsn, fmt.Sprintf("GRANT %s TO CURRENT_USER WITH SET TRUE", otherAdminName))
			dbExecute(t, dsn, fmt.Sprintf(
				"SET ROLE %s; GRANT %s TO %s WITH ADMIN OPTION; RESET ROLE",
				otherAdminName, grantedName, memberName,
			))

			// A CREATEROLE non-superuser is granted the roles it creates, so step out
			// of the competing grantor to make it genuinely foreign to this session.
			dbExecute(t, dsn, fmt.Sprintf("REVOKE %s FROM CURRENT_USER", otherAdminName))
		},
		Providers: testAccProviders,
		CheckDestroy: func(s *terraform.State) error {
			for _, role := range []string{memberName, grantedName, otherAdminName} {
				dbExecute(t, dsn, fmt.Sprintf("DROP ROLE IF EXISTS %s", role))
			}
			return nil
		},
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
				resource postgresql_grant_role "grant_role" {
					role       = "%s"
					grant_role = "%s"
				}
				`, memberName, grantedName),
				Check: resource.ComposeTestCheckFunc(
					// The competing grant carries admin option; ours does not, and it is
					// ours that this resource represents.
					resource.TestCheckResourceAttr(
						"postgresql_grant_role.grant_role", "with_admin_option", strconv.FormatBool(false)),
					resource.TestCheckResourceAttr(
						"postgresql_grant_role.grant_role", "id", fmt.Sprintf("%s_%s_false", memberName, grantedName)),
				),
			},
			{
				// The read must settle: reporting the foreign grant's admin_option here
				// forces replacement on every plan, which is the bug this covers.
				Config: fmt.Sprintf(`
				resource postgresql_grant_role "grant_role" {
					role       = "%s"
					grant_role = "%s"
				}
				`, memberName, grantedName),
				PlanOnly: true,
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
		defer closeDB(t, db)

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
