package postgresql

import (
	"database/sql"
	"fmt"
	// "strconv"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	// "github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
	// "github.com/lib/pq"
)

func TestAccPostgresqlPermissions(t *testing.T) {
	skipIfNotAcc(t)

	config := getTestConfig(t)
	dsn := config.connStr("postgres")

	dbSuffix, teardown := setupTestDatabase(t, false, true)
	defer teardown()

	_, roleName := getTestDBNames(dbSuffix)

	// Configure test resource with dynamic values if needed
	testAccPostgresqlPermissionsResource := fmt.Sprintf(`
resource "postgresql_permissions" "test" {
	role         = "%s"
	create_db    = true
	create_role  = false
}
`, roleName)

	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccPreCheck(t)
			testCheckCompatibleVersion(t, featurePrivileges)
		},
		Providers: testAccProviders,
		Steps: []resource.TestStep{
			{
				Config: testAccPostgresqlPermissionsResource,
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(
						"postgresql_permissions.test", "role", roleName),
					resource.TestCheckResourceAttr(
						"postgresql_permissions.test", "create_db", "true"),
					resource.TestCheckResourceAttr(
						"postgresql_permissions.test", "create_role", "false"),
					testCheckPostgresqlPermissions(t, dsn, roleName, true, false),
				),
			},
			// You can add more TestSteps here to test updates or deletion
		},
	})
}

// testCheckPostgresqlPermissions constructs a resource.TestCheckFunc to validate
// the permissions of a PostgreSQL role. You need to replace 'dsn' with your actual DSN or
// configuration logic to connect to your test PostgreSQL instance.
func testCheckPostgresqlPermissions(t *testing.T, dsn, roleName string, createDb, createRole bool) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		// Replace 'dsn' with actual logic to obtain DSN for your test database
		db, err := sql.Open("postgres", dsn)
		if err != nil {
			t.Fatalf("could to create connection pool: %v", err)
		}
		defer db.Close()

		var rolcreatedb, rolcreaterole sql.NullBool
		err = db.QueryRow(`SELECT rolcreatedb, rolcreaterole FROM pg_roles WHERE rolname = $1`, roleName).Scan(&rolcreatedb, &rolcreaterole)
		if err != nil {
			return fmt.Errorf("could not query role permissions: %v", err)
		}

		if rolcreatedb.Valid && rolcreatedb.Bool != createDb {
			return fmt.Errorf("expected create_db to be %v for role %s, got %v", createDb, roleName, rolcreatedb.Bool)
		}

		if rolcreaterole.Valid && rolcreaterole.Bool != createRole {
			return fmt.Errorf("expected create_role to be %v for role %s, got %v", createRole, roleName, rolcreaterole.Bool)
		}

		return nil
	}
}
