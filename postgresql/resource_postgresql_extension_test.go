package postgresql

import (
	"database/sql"
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/terraform"
)

func TestAccPostgresqlExtension_Basic(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccPreCheck(t)
			testCheckCompatibleVersion(t, featureExtension)
			// TODO: Need to check how RDS manage to allow `rds_supuser` to create extension
			// even it's not a real superuser
			testSuperuserPreCheck(t)
		},
		Providers:    testAccProviders,
		CheckDestroy: testAccCheckPostgresqlExtensionDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccPostgresqlExtensionConfig,
				Check: resource.ComposeTestCheckFunc(
					testAccCheckPostgresqlExtensionExists("postgresql_extension.myextension"),
					resource.TestCheckResourceAttr(
						"postgresql_extension.myextension", "name", "pg_trgm"),
					resource.TestCheckResourceAttr(
						"postgresql_extension.myextension", "schema", "public"),

					// NOTE(sean): The version number drifts.  PG 9.6 ships with pg_trgm
					// version 1.3 and PG 9.2 ships with pg_trgm 1.0.
					resource.TestCheckResourceAttrSet(
						"postgresql_extension.myextension", "version"),
				),
			},
		},
	})
}

func testAccCheckPostgresqlExtensionDestroy(s *terraform.State) error {
	client := testAccProvider.Meta().(*Client)

	for _, rs := range s.RootModule().Resources {
		if rs.Type != "postgresql_extension" {
			continue
		}

		database, ok := rs.Primary.Attributes[extDatabaseAttr]
		if !ok {
			return fmt.Errorf("No Attribute for database is set")
		}
		txn, err := startTransaction(client, database)
		if err != nil {
			return err
		}
		defer deferredRollback(txn)

		exists, err := checkExtensionExists(txn, getExtensionNameFromID(rs.Primary.ID))

		if err != nil {
			return fmt.Errorf("Error checking extension %s", err)
		}

		if exists {
			return fmt.Errorf("Extension still exists after destroy")
		}
	}

	return nil
}

func testAccCheckPostgresqlExtensionExists(n string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[n]
		if !ok {
			return fmt.Errorf("Resource not found: %s", n)
		}

		if rs.Primary.ID == "" {
			return fmt.Errorf("No ID is set")
		}

		database, ok := rs.Primary.Attributes[extDatabaseAttr]
		if !ok {
			return fmt.Errorf("No Attribute for database is set")
		}

		extName, ok := rs.Primary.Attributes[extNameAttr]
		if !ok {
			return fmt.Errorf("No Attribute for extension name is set")
		}

		client := testAccProvider.Meta().(*Client)
		txn, err := startTransaction(client, database)
		if err != nil {
			return err
		}
		defer deferredRollback(txn)

		exists, err := checkExtensionExists(txn, extName)

		if err != nil {
			return fmt.Errorf("Error checking extension %s", err)
		}

		if !exists {
			return fmt.Errorf("Extension not found")
		}

		return nil
	}
}

func TestAccPostgresqlExtension_SchemaRename(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccPreCheck(t)
			testCheckCompatibleVersion(t, featureExtension)
			testSuperuserPreCheck(t)
		},
		Providers:    testAccProviders,
		CheckDestroy: testAccCheckPostgresqlExtensionDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccPostgresqlExtensionSchemaChange1,
				Check: resource.ComposeTestCheckFunc(
					testAccCheckPostgresqlExtensionExists("postgresql_extension.ext1trgm"),
					resource.TestCheckResourceAttr(
						"postgresql_schema.ext1foo", "name", "foo"),
					resource.TestCheckResourceAttr(
						"postgresql_extension.ext1trgm", "name", "pg_trgm"),
					resource.TestCheckResourceAttr(
						"postgresql_extension.ext1trgm", "name", "pg_trgm"),
					resource.TestCheckResourceAttr(
						"postgresql_extension.ext1trgm", "schema", "foo"),
				),
			},
			{
				Config: testAccPostgresqlExtensionSchemaChange2,
				Check: resource.ComposeTestCheckFunc(
					testAccCheckPostgresqlExtensionExists("postgresql_extension.ext1trgm"),
					resource.TestCheckResourceAttr(
						"postgresql_schema.ext1foo", "name", "bar"),
					resource.TestCheckResourceAttr(
						"postgresql_extension.ext1trgm", "name", "pg_trgm"),
					resource.TestCheckResourceAttr(
						"postgresql_extension.ext1trgm", "schema", "bar"),
				),
			},
		},
	})
}

func checkExtensionExists(txn *sql.Tx, extensionName string) (bool, error) {
	var _rez bool
	err := txn.QueryRow("SELECT TRUE from pg_catalog.pg_extension d WHERE extname=$1", extensionName).Scan(&_rez)
	switch {
	case err == sql.ErrNoRows:
		return false, nil
	case err != nil:
		return false, fmt.Errorf("Error reading info about extension: %s", err)
	}

	return true, nil
}

func TestAccPostgresqlExtension_Database(t *testing.T) {
	skipIfNotAcc(t)

	dbSuffix, teardown := setupTestDatabase(t, true, true)
	defer teardown()

	dbName, _ := getTestDBNames(dbSuffix)

	testAccPostgresqlExtensionDatabaseConfig := fmt.Sprintf(`
	resource "postgresql_extension" "myextension" {
		name     = "pg_trgm"
		database = "%s"
	}
	`, dbName)

	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccPreCheck(t)
			testCheckCompatibleVersion(t, featureExtension)
			testSuperuserPreCheck(t)
		},
		Providers:    testAccProviders,
		CheckDestroy: testAccCheckPostgresqlExtensionDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccPostgresqlExtensionDatabaseConfig,
				Check: resource.ComposeTestCheckFunc(
					testAccCheckPostgresqlExtensionExists("postgresql_extension.myextension"),
					resource.TestCheckResourceAttr(
						"postgresql_extension.myextension", "name", "pg_trgm"),
					resource.TestCheckResourceAttr(
						"postgresql_extension.myextension", "schema", "public"),
					resource.TestCheckResourceAttr(
						"postgresql_extension.myextension", "database", dbName),
					resource.TestCheckResourceAttrSet(
						"postgresql_extension.myextension", "version"),
				),
			},
		},
	})
}

func TestAccPostgresqlExtension_DropCascade(t *testing.T) {
	skipIfNotAcc(t)

	var testAccPostgresqlExtensionConfig = `
resource "postgresql_extension" "cascade" {
  name = "pgcrypto"
  drop_cascade = true
}
`
	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccPreCheck(t)
			testCheckCompatibleVersion(t, featureExtension)
			testSuperuserPreCheck(t)
		},
		Providers:    testAccProviders,
		CheckDestroy: testAccCheckPostgresqlExtensionDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccPostgresqlExtensionConfig,
				Check: resource.ComposeTestCheckFunc(
					testAccCheckPostgresqlExtensionExists("postgresql_extension.cascade"),
					resource.TestCheckResourceAttr("postgresql_extension.cascade", "name", "pgcrypto"),
					// This will create a dependency on the extension.
					testAccCreateExtensionDependency("test_extension_cascade"),
				),
			},
		},
	})
}

func testAccCreateExtensionDependency(tableName string) resource.TestCheckFunc {
	return func(s *terraform.State) error {

		client := testAccProvider.Meta().(*Client)
		db, err := client.Connect()
		if err != nil {
			return err
		}

		_, err = db.Exec(fmt.Sprintf("DROP TABLE IF EXISTS %s; CREATE TABLE %s (id uuid DEFAULT gen_random_uuid())", tableName, tableName))
		if err != nil {
			return fmt.Errorf("could not create test table in schema: %s", err)
		}

		return nil
	}
}

func TestAccPostgresqlExtension_CreateCascade(t *testing.T) {
	skipIfNotAcc(t)

	var testAccPostgresqlExtensionConfig = `
resource "postgresql_extension" "cascade" {
  name = "earthdistance"
  create_cascade = true
}
`
	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccPreCheck(t)
			testCheckCompatibleVersion(t, featureExtension)
			testSuperuserPreCheck(t)
		},
		Providers:    testAccProviders,
		CheckDestroy: testAccCheckPostgresqlExtensionDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccPostgresqlExtensionConfig,
				Check: resource.ComposeTestCheckFunc(
					testAccCheckPostgresqlExtensionExists("postgresql_extension.cascade"),
					resource.TestCheckResourceAttr("postgresql_extension.cascade", "name", "earthdistance"),
					// The cube extension should be installed as a dependency of earthdistance.
					testAccCheckExtensionDependency("cube"),
				),
			},
		},
	})
}

func testAccCheckExtensionDependency(extName string) resource.TestCheckFunc {
	return func(s *terraform.State) error {

		client := testAccProvider.Meta().(*Client)
		txn, err := startTransaction(client, client.databaseName)
		if err != nil {
			return err
		}
		defer deferredRollback(txn)

		exists, err := checkExtensionExists(txn, extName)
		if err != nil {
			return fmt.Errorf("Error checking extension %s", err)
		}
		if !exists {
			return fmt.Errorf("Extension %s not found", extName)
		}

		return nil
	}
}

var testAccPostgresqlExtensionConfig = `
resource "postgresql_extension" "myextension" {
  name = "pg_trgm"
}
`

var testAccPostgresqlExtensionSchemaChange1 = `
resource "postgresql_schema" "ext1foo" {
  name = "foo"
}

resource "postgresql_extension" "ext1trgm" {
  name = "pg_trgm"
  schema = "${postgresql_schema.ext1foo.name}"
}
`

var testAccPostgresqlExtensionSchemaChange2 = `
resource "postgresql_schema" "ext1foo" {
  name = "bar"
}

resource "postgresql_extension" "ext1trgm" {
  name = "pg_trgm"
  schema = "${postgresql_schema.ext1foo.name}"
}
`
