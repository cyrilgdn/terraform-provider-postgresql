package postgresql

import (
	"database/sql"
	"fmt"
	"os"
	"strconv"
	"testing"

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
		Database: getEnv("PGDATABASE", "postgres"),
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
