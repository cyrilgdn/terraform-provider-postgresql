package postgresql

import (
	"database/sql"
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
	"github.com/lib/pq"
)

func TestCreateAlterRoleQuery(t *testing.T) {
	var roleName = "foo"
	var parameterKey = "log_statement"
	var parameterValue = "ALL"

	cases := []struct {
		resource map[string]interface{}
		expected string
	}{
		{
			resource: map[string]interface{}{
				"role_name":       roleName,
				"parameter_key":   parameterKey,
				"parameter_value": parameterValue,
			},
			expected: fmt.Sprintf("ALTER ROLE %s SET %s TO %s",
				pq.QuoteIdentifier(roleName),
				pq.QuoteIdentifier(parameterKey),
				pq.QuoteIdentifier(parameterValue)),
		},
	}

	for _, c := range cases {
		out := createAlterRoleQuery(schema.TestResourceDataRaw(t, resourcePostgreSQLAlterRole().Schema, c.resource))
		if out != c.expected {
			t.Fatalf("Error matching output and expected: %#v vs %#v", out, c.expected)
		}
	}
}

func TestResetRoleQuery(t *testing.T) {
	var roleName = "foo"
	var parameterKey = "log_statement"

	expected := fmt.Sprintf("ALTER ROLE %s RESET %s", pq.QuoteIdentifier(roleName), pq.QuoteIdentifier(parameterKey))

	cases := []struct {
		resource map[string]interface{}
	}{
		{
			resource: map[string]interface{}{
				"role_name":     roleName,
				"parameter_key": parameterKey,
			},
		},
	}

	for _, c := range cases {
		out := createResetAlterRoleQuery(schema.TestResourceDataRaw(t, resourcePostgreSQLAlterRole().Schema, c.resource))
		if out != expected {
			t.Fatalf("Error matching output and expected: %#v vs %#v", out, expected)
		}
	}
}

func TestAccPostgresqlAlterRole(t *testing.T) {
	skipIfNotAcc(t)

	config := getTestConfig(t)
	dsn := config.connStr("postgres")

	dbSuffix, teardown := setupTestDatabase(t, false, true)
	defer teardown()

	_, roleName := getTestDBNames(dbSuffix)

	parameterKey := "log_statement"
	parameterValue := "ALL"

	testAccPostgresqlAlterRoleResources := fmt.Sprintf(`
	resource "postgresql_alter_role" "alter_role" {
		role_name = "%s"
		parameter_key = "%s"
		parameter_value = "%s"
	}
	`, roleName, parameterKey, parameterValue)

	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccPreCheck(t)
			testCheckCompatibleVersion(t, featurePrivileges)
			testSuperuserPreCheck(t)
		},
		Providers: testAccProviders,
		Steps: []resource.TestStep{
			{
				Config: testAccPostgresqlAlterRoleResources,
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(
						"postgresql_alter_role.alter_role", "role_name", roleName),
					resource.TestCheckResourceAttr(
						"postgresql_alter_role.alter_role", "parameter_key", parameterKey),
					resource.TestCheckResourceAttr(
						"postgresql_alter_role.alter_role", "parameter_value", parameterValue),
					checkAlterRole(t, dsn, roleName, parameterKey, parameterValue),
				),
			},
		},
	})
}

func checkAlterRole(t *testing.T, dsn, role string, parameterKey string, parameterValue string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		db, err := sql.Open("postgres", dsn)
		if err != nil {
			t.Fatalf("could to create connection pool: %v", err)
		}
		defer db.Close()

		roleParameter := fmt.Sprintf("%s=%s", parameterKey, parameterValue)
		var _rez int
		err = db.QueryRow(`
		SELECT 1
		FROM pg_catalog.pg_roles
		WHERE rolname = $1
		AND $2=ANY(rolconfig)
		`, role, roleParameter).Scan(&_rez)

		switch {
		case err == sql.ErrNoRows:
			return fmt.Errorf(
				"Role %s does not have the following attribute assigned %s",
				role, roleParameter,
			)

		case err != nil:
			t.Fatalf("could not check role attributes: %v", err)
		}

		return nil
	}
}
