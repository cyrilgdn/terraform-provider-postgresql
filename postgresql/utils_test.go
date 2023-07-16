package postgresql

import (
	"database/sql"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
)

const (
	dbNamePrefix     = "tf_tests_db"
	roleNamePrefix   = "tf_tests_role"
	testRolePassword = "testpwd"
)

// Can be used in a PreCheck function to disable test based on feature.
func testCheckCompatibleVersion(t *testing.T, feature featureName) {
	client := testAccProvider.Meta().(*Client)
	db, err := client.Connect()
	if err != nil {
		t.Fatalf("could connect to database: %v", err)
	}
	if !db.featureSupported(feature) {
		t.Skipf("Skip extension tests for Postgres %s", db.version)
	}
}

// Some tests have to be run as a real superuser (not RDS like)
func testSuperuserPreCheck(t *testing.T) {
	client := testAccProvider.Meta().(*Client)
	if !client.config.Superuser {
		t.Skip("Skip test: This test can be run only with a real superuser")
	}
}

func getTestConfig(t *testing.T) Config {
	getEnv := func(key, fallback string) string {
		value := os.Getenv(key)
		if len(value) == 0 {
			return fallback
		}
		return value
	}

	dbPort, err := strconv.Atoi(getEnv("PGPORT", "5432"))
	if err != nil {
		t.Fatalf("could not cast PGPORT value as integer: %v", err)
	}

	return Config{
		Scheme:   "postgres",
		Host:     getEnv("PGHOST", "localhost"),
		Port:     dbPort,
		Username: getEnv("PGUSER", ""),
		Password: getEnv("PGPASSWORD", ""),
		SSLMode:  getEnv("PGSSLMODE", ""),
	}
}

func skipIfNotAcc(t *testing.T) {
	if os.Getenv(resource.EnvTfAcc) == "" {
		t.Skipf("Acceptance tests skipped unless env '%s' set", resource.EnvTfAcc)
	}
}

// Skip tests on RDS like environments
func skipIfNotSuperuser(t *testing.T) {
	if os.Getenv("PGSUPERUSER") == "false" {
		t.Skip("Acceptance tests skipped due to lack of real superuser privileges")
	}
}

// dbExecute is a test helper to create a pool, execute one query then close the pool
func dbExecute(t *testing.T, dsn, query string, args ...interface{}) {
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatalf("could to create connection pool: %v", err)
	}
	defer db.Close()

	// Create the test DB
	if _, err = db.Exec(query, args...); err != nil {
		t.Fatalf("could not execute query %s: %v", query, err)
	}
}

func getTestDBNames(dbSuffix string) (dbName string, roleName string) {
	dbName = fmt.Sprintf("%s_%s", dbNamePrefix, dbSuffix)
	roleName = fmt.Sprintf("%s_%s", roleNamePrefix, dbSuffix)

	return
}

// setupTestDatabase creates all needed resources before executing a terraform test
// and provides the teardown function to delete all these resources.
func setupTestDatabase(t *testing.T, createDB, createRole bool) (string, func()) {
	config := getTestConfig(t)

	suffix := strconv.Itoa(int(time.Now().UnixNano()))

	dbName, roleName := getTestDBNames(suffix)

	if createRole {
		dbExecute(t, config.connStr("postgres"), fmt.Sprintf(
			"CREATE ROLE %s LOGIN ENCRYPTED PASSWORD '%s'",
			roleName, testRolePassword,
		))
	}

	if createDB {
		dbExecute(t, config.connStr("postgres"), fmt.Sprintf("CREATE DATABASE %s", dbName))
		// Create a test schema in this new database and grant usage to rolName
		dbExecute(t, config.connStr(dbName), "CREATE SCHEMA test_schema")
		dbExecute(t, config.connStr(dbName), fmt.Sprintf("GRANT usage ON SCHEMA test_schema to %s", roleName))
		dbExecute(t, config.connStr(dbName), "CREATE SCHEMA dev_schema")
		dbExecute(t, config.connStr(dbName), fmt.Sprintf("GRANT usage ON SCHEMA dev_schema to %s", roleName))
	}

	return suffix, func() {
		dbExecute(t, config.connStr("postgres"), fmt.Sprintf("DROP DATABASE IF EXISTS %s", dbName))
		dbExecute(t, config.connStr("postgres"), fmt.Sprintf("DROP ROLE IF EXISTS %s", roleName))
	}
}

// createTestRole creates a role before executing a terraform test
// and provides the teardown function to delete all these resources.
func createTestRole(t *testing.T, roleName string) func() {
	config := getTestConfig(t)

	dbExecute(t, config.connStr("postgres"), fmt.Sprintf(
		"CREATE ROLE %s LOGIN ENCRYPTED PASSWORD '%s'",
		roleName, testRolePassword,
	))

	return func() {
		dbExecute(t, config.connStr("postgres"), fmt.Sprintf("DROP ROLE IF EXISTS %s", roleName))
	}
}

func createTestTables(t *testing.T, dbSuffix string, tables []string, owner string) func() {
	config := getTestConfig(t)
	dbName, _ := getTestDBNames(dbSuffix)
	adminUser := config.getDatabaseUsername()

	db, err := sql.Open("postgres", config.connStr(dbName))
	if err != nil {
		t.Fatalf("could not open connection pool for db %s: %v", dbName, err)
	}
	defer db.Close()

	if owner != "" {
		if !config.Superuser {
			if _, err := db.Exec(fmt.Sprintf("GRANT %s TO CURRENT_USER", owner)); err != nil {
				t.Fatalf("could not grant role %s to current user: %v", owner, err)
			}
		}
		if _, err := db.Exec(fmt.Sprintf("SET ROLE %s", owner)); err != nil {
			t.Fatalf("could not set role to %s: %v", owner, err)
		}
	}

	for _, table := range tables {
		if _, err := db.Exec(fmt.Sprintf("CREATE TABLE %s (val text, test_column_one text, test_column_two text)", table)); err != nil {
			t.Fatalf("could not create test table in db %s: %v", dbName, err)
		}
		if owner != "" {
			if _, err := db.Exec(fmt.Sprintf("ALTER TABLE %s OWNER TO %s", table, owner)); err != nil {
				t.Fatalf("could not set test_table owner to %s: %v", owner, err)
			}
		}
	}
	if owner != "" && !config.Superuser {
		if _, err := db.Exec(fmt.Sprintf("SET ROLE %s; REVOKE %s FROM %s", adminUser, owner, adminUser)); err != nil {
			t.Fatalf("could not revoke role %s from %s: %v", owner, adminUser, err)
		}
	}

	// In this case we need to drop table after each test.
	return func() {
		db, err := sql.Open("postgres", config.connStr(dbName))
		if err != nil {
			t.Fatalf("could not open connection pool for db %s: %v", dbName, err)
		}
		defer db.Close()

		if owner != "" && !config.Superuser {
			if _, err := db.Exec(fmt.Sprintf("GRANT %s TO CURRENT_USER", owner)); err != nil {
				t.Fatalf("could not grant role %s to current user: %v", owner, err)
			}
		}

		for _, table := range tables {
			if _, err := db.Exec(fmt.Sprintf("DROP TABLE %s", table)); err != nil {
				t.Fatalf("could not drop table %s: %v", table, err)
			}
		}
		if owner != "" && !config.Superuser {
			if _, err := db.Exec(fmt.Sprintf("SET ROLE %s; REVOKE %s FROM %s", adminUser, owner, adminUser)); err != nil {
				t.Fatalf("could not revoke role %s from %s: %v", owner, adminUser, err)
			}
		}

	}
}

func createTestSchemas(t *testing.T, dbSuffix string, schemas []string, owner string) func() {
	config := getTestConfig(t)
	dbName, _ := getTestDBNames(dbSuffix)
	adminUser := config.getDatabaseUsername()

	db, err := sql.Open("postgres", config.connStr(dbName))
	if err != nil {
		t.Fatalf("could not open connection pool for db %s: %v", dbName, err)
	}
	defer db.Close()

	if owner != "" {
		if !config.Superuser {
			if _, err := db.Exec(fmt.Sprintf("GRANT %s TO CURRENT_USER", owner)); err != nil {
				t.Fatalf("could not grant role %s to current user: %v", owner, err)
			}
		}
		if _, err := db.Exec(fmt.Sprintf("SET ROLE %s", owner)); err != nil {
			t.Fatalf("could not set role to %s: %v", owner, err)
		}
	}

	for _, schema := range schemas {
		if _, err := db.Exec(fmt.Sprintf("CREATE SCHEMA %s", schema)); err != nil {
			t.Fatalf("could not create test schema in db %s: %v", dbName, err)
		}
		if owner != "" {
			if _, err := db.Exec(fmt.Sprintf("ALTER SCHEMA %s OWNER TO %s", schema, owner)); err != nil {
				t.Fatalf("could not set test schema owner to %s: %v", owner, err)
			}
		}
	}
	if owner != "" && !config.Superuser {
		if _, err := db.Exec(fmt.Sprintf("SET ROLE %s; REVOKE %s FROM %s", adminUser, owner, adminUser)); err != nil {
			t.Fatalf("could not revoke role %s from %s: %v", owner, adminUser, err)
		}
	}

	// In this case we need to drop schema after each test.
	return func() {
		db, err := sql.Open("postgres", config.connStr(dbName))
		if err != nil {
			t.Fatalf("could not open connection pool for db %s: %v", dbName, err)
		}
		defer db.Close()

		if owner != "" && !config.Superuser {
			if _, err := db.Exec(fmt.Sprintf("GRANT %s TO CURRENT_USER", owner)); err != nil {
				t.Fatalf("could not grant role %s to current user: %v", owner, err)
			}
		}

		for _, schema := range schemas {
			if _, err := db.Exec(fmt.Sprintf("DROP SCHEMA %s CASCADE", schema)); err != nil {
				t.Fatalf("could not drop schema %s: %v", schema, err)
			}
		}
		if owner != "" && !config.Superuser {
			if _, err := db.Exec(fmt.Sprintf("SET ROLE %s; REVOKE %s FROM %s", adminUser, owner, adminUser)); err != nil {
				t.Fatalf("could not revoke role %s from %s: %v", owner, adminUser, err)
			}
		}
	}
}

func createTestSequences(t *testing.T, dbSuffix string, sequences []string, owner string) func() {
	config := getTestConfig(t)
	dbName, _ := getTestDBNames(dbSuffix)
	adminUser := config.getDatabaseUsername()

	db, err := sql.Open("postgres", config.connStr(dbName))
	if err != nil {
		t.Fatalf("could not open connection pool for db %s: %v", dbName, err)
	}
	defer db.Close()

	if owner != "" {
		if !config.Superuser {
			if _, err := db.Exec(fmt.Sprintf("GRANT %s TO CURRENT_USER", owner)); err != nil {
				t.Fatalf("could not grant role %s to current user: %v", owner, err)
			}
		}
		if _, err := db.Exec(fmt.Sprintf("SET ROLE %s", owner)); err != nil {
			t.Fatalf("could not set role to %s: %v", owner, err)
		}
	}

	for _, sequence := range sequences {
		if _, err := db.Exec(fmt.Sprintf("CREATE sequence %s", sequence)); err != nil {
			t.Fatalf("could not create test sequence in db %s: %v", dbName, err)
		}
		if owner != "" {
			if _, err := db.Exec(fmt.Sprintf("ALTER sequence %s OWNER TO %s", sequence, owner)); err != nil {
				t.Fatalf("could not set test_sequence owner to %s: %v", owner, err)
			}
		}
	}
	if owner != "" && !config.Superuser {
		if _, err := db.Exec(fmt.Sprintf("SET ROLE %s; REVOKE %s FROM %s", adminUser, owner, adminUser)); err != nil {
			t.Fatalf("could not revoke role %s from %s: %v", owner, adminUser, err)
		}
	}

	return func() {
		db, err := sql.Open("postgres", config.connStr(dbName))
		if err != nil {
			t.Fatalf("could not open connection pool for db %s: %v", dbName, err)
		}
		defer db.Close()

		if owner != "" && !config.Superuser {
			if _, err := db.Exec(fmt.Sprintf("GRANT %s TO CURRENT_USER", owner)); err != nil {
				t.Fatalf("could not grant role %s to current user: %v", owner, err)
			}
		}

		for _, sequence := range sequences {
			if _, err := db.Exec(fmt.Sprintf("DROP sequence %s", sequence)); err != nil {
				t.Fatalf("could not drop table %s: %v", sequence, err)
			}
		}
		if owner != "" && !config.Superuser {
			if _, err := db.Exec(fmt.Sprintf("SET ROLE %s; REVOKE %s FROM %s", adminUser, owner, adminUser)); err != nil {
				t.Fatalf("could not revoke role %s from %s: %v", owner, adminUser, err)
			}
		}

	}
}

// testHasGrantForQuery executes a query and checks that it fails if
// we were not allowed or succeses if we're allowed.
func testHasGrantForQuery(db *sql.DB, query string, allowed bool) error {
	_, err := db.Exec(query)
	if err != nil {
		if allowed {
			return fmt.Errorf("could not execute %s as expected: %w", query, err)
		}
		return nil
	}

	if !allowed {
		return fmt.Errorf("did not fail as expected when executing query '%s'", query)
	}
	return nil
}

func connectAsTestRole(t *testing.T, role, dbName string) *sql.DB {
	config := getTestConfig(t)

	// Connect as the test role
	config.Username = role
	config.Password = testRolePassword

	db, err := sql.Open("postgres", config.connStr(dbName))
	if err != nil {
		t.Fatalf("could not open connection pool for db %s: %v", dbName, err)
	}
	return db
}

func testCheckTablesPrivileges(t *testing.T, dbName, roleName string, tables []string, allowedPrivileges []string) error {
	db := connectAsTestRole(t, roleName, dbName)
	defer db.Close()

	for _, table := range tables {
		queries := map[string]string{
			"SELECT": fmt.Sprintf("SELECT count(*) FROM %s", table),
			"INSERT": fmt.Sprintf("INSERT INTO %s VALUES ('test')", table),
			"UPDATE": fmt.Sprintf("UPDATE %s SET val = 'test'", table),
			"DELETE": fmt.Sprintf("DELETE FROM %s", table),
		}

		for queryType, query := range queries {
			if err := testHasGrantForQuery(db, query, sliceContainsStr(allowedPrivileges, queryType)); err != nil {
				return err
			}
		}
	}
	return nil
}

func testCheckSchemasPrivileges(t *testing.T, dbName, roleName string, schemas []string, allowedPrivileges []string) error {
	db := connectAsTestRole(t, roleName, dbName)
	defer db.Close()

	for _, schema := range schemas {
		queries := map[string]string{
			"USAGE":  fmt.Sprintf("DROP TABLE IF EXISTS %s.test_table", schema),
			"CREATE": fmt.Sprintf("CREATE TABLE %s.test_table()", schema),
		}

		for queryType, query := range queries {
			if err := testHasGrantForQuery(db, query, sliceContainsStr(allowedPrivileges, queryType)); err != nil {
				return err
			}
		}
	}
	return nil
}

func testCheckColumnPrivileges(t *testing.T, dbName, roleName string, tables []string, allowedPrivileges []string, columns []string) error {
	db := connectAsTestRole(t, roleName, dbName)
	defer db.Close()

	columnValues := []string{}
	for _, col := range columns {
		columnValues = append(columnValues, fmt.Sprint("'", col, "'"))
	}

	updateColumnValues := []string{}
	for i := range columns {
		updateColumnValues = append(updateColumnValues, fmt.Sprint(columns[i], " = ", columnValues[i]))
	}

	for _, table := range tables {
		queries := map[string]string{
			"SELECT": fmt.Sprintf("SELECT %s FROM %s", strings.Join(columns, ", "), table),
			"INSERT": fmt.Sprintf("INSERT INTO %s(%s) VALUES (%s)", table, strings.Join(columns, ", "), strings.Join(columnValues, ", ")),
			"UPDATE": fmt.Sprintf("UPDATE %s SET %s", table, strings.Join(updateColumnValues, ", ")),
		}

		for queryType, query := range queries {
			if err := testHasGrantForQuery(db, query, sliceContainsStr(allowedPrivileges, queryType)); err != nil {
				return err
			}
		}
	}
	return nil
}
