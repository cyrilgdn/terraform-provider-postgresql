package postgresql

import (
	"database/sql"
	"fmt"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/hashicorp/errwrap"
	"github.com/hashicorp/terraform/helper/resource"
)

const (
	dbNamePrefix     = "tf_tests_db"
	roleNamePrefix   = "tf_tests_role"
	testRolePassword = "testpwd"

	testTableDef = "CREATE TABLE test_table (val text)"
)

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
		Host:     getEnv("PGHOST", "localhost"),
		Port:     dbPort,
		Username: getEnv("PGUSER", ""),
		Password: getEnv("PGPASSWORD", ""),
		SSLMode:  getEnv("PGSSLMODE", ""),
	}
}

func skipIfNotAcc(t *testing.T) {
	if os.Getenv(resource.TestEnvVar) == "" {
		t.Skip(fmt.Sprintf(
			"Acceptance tests skipped unless env '%s' set",
			resource.TestEnvVar))
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
func setupTestDatabase(t *testing.T, createDB, createRole, createTable bool) (string, func()) {
	config := getTestConfig(t)

	suffix := strconv.Itoa(int(time.Now().UnixNano()))

	dbName, roleName := getTestDBNames(suffix)

	if createDB {
		dbExecute(t, config.connStr("postgres"), fmt.Sprintf("CREATE DATABASE %s", dbName))
	}
	if createRole {
		dbExecute(t, config.connStr("postgres"), fmt.Sprintf(
			"CREATE ROLE %s LOGIN ENCRYPTED PASSWORD '%s'",
			roleName, testRolePassword,
		))
	}

	if createTable {
		// Create a test table in this new database
		dbExecute(t, config.connStr(dbName), testTableDef)
	}

	return suffix, func() {
		dbExecute(t, config.connStr("postgres"), fmt.Sprintf("DROP DATABASE IF EXISTS %s", dbName))
		dbExecute(t, config.connStr("postgres"), fmt.Sprintf("DROP ROLE IF EXISTS %s", roleName))
	}
}

func testCheckTablePrivileges(
	t *testing.T, dbSuffix string, allowedPrivileges []string, createTable bool,
) error {
	config := getTestConfig(t)

	dbName, roleName := getTestDBNames(dbSuffix)

	// Some test (e.g.: default privileges) need the test table to be created only now
	if createTable {
		db, err := sql.Open("postgres", config.connStr(dbName))
		if err != nil {
			t.Fatalf("could not open connection pool for db %s: %v", dbName, err)
		}
		defer db.Close()

		if _, err := db.Exec(testTableDef); err != nil {
			t.Fatalf("could not create test table in db %s: %v", dbName, err)
		}
		// In this case we need to drop table after each test.
		defer func() {
			db.Exec("DROP TABLE test_table")
		}()
	}

	// Connect as the test role
	config.Username = roleName
	config.Password = testRolePassword

	db, err := sql.Open("postgres", config.connStr(dbName))
	if err != nil {
		t.Fatalf("could not open connection pool for db %s: %v", dbName, err)
	}
	defer db.Close()

	queries := map[string]string{
		"SELECT": "SELECT count(*) FROM test_table",
		"INSERT": "INSERT INTO test_table VALUES ('test')",
		"UPDATE": "UPDATE test_table SET val = 'test'",
		"DELETE": "DELETE FROM test_table",
	}

	for queryType, query := range queries {
		_, err := db.Exec(query)

		if err != nil && sliceContainsStr(allowedPrivileges, queryType) {
			return errwrap.Wrapf(fmt.Sprintf("could not %s on test table: {{err}}", queryType), err)

		} else if err == nil && !sliceContainsStr(allowedPrivileges, queryType) {
			return errwrap.Wrapf(fmt.Sprintf("%s did not failed as expected: {{err}}", queryType), err)
		}
	}
	return nil
}
