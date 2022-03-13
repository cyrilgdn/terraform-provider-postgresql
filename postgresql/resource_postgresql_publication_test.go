package postgresql

import (
	"database/sql"
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
)

func TestAccPostgresqlPublication_Basic(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccPreCheck(t)
			testSuperuserPreCheck(t)
		},
		Providers:    testAccProviders,
		CheckDestroy: testAccCheckPostgresqlPublicationDestroy,
		Steps: []resource.TestStep{
			{
				Config: `
				resource "postgresql_publication" "test" {
					name   = "publication"
				}`,
				Check: resource.ComposeTestCheckFunc(
					testAccCheckPostgresqlPublicationExists("postgresql_publication.test"),
					resource.TestCheckResourceAttr(
						"postgresql_publication.test", "name", "publication"),
					resource.TestCheckResourceAttr(
						"postgresql_publication.test", "database", "postgres"),
				),
			},
		},
	})
}

func testAccCheckPostgresqlPublicationDestroy(s *terraform.State) error {
	client := testAccProvider.Meta().(*Client)

	for _, rs := range s.RootModule().Resources {
		if rs.Type != "postgresql_publication" {
			continue
		}

		database, ok := rs.Primary.Attributes[pubDatabaseAttr]
		if !ok {
			return fmt.Errorf("No Attribute for database is set")
		}
		txn, err := startTransaction(client, database)
		if err != nil {
			return err
		}
		defer deferredRollback(txn)

		exists, err := checkPublicationExists(txn, getPublicationNameFromID(rs.Primary.ID))

		if err != nil {
			return fmt.Errorf("Error checking publication %s", err)
		}

		if exists {
			return fmt.Errorf("Publication still exists after destroy")
		}
	}

	return nil
}

func testAccCheckPostgresqlPublicationExists(n string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[n]
		if !ok {
			return fmt.Errorf("Resource not found: %s", n)
		}

		if rs.Primary.ID == "" {
			return fmt.Errorf("No ID is set")
		}

		database, ok := rs.Primary.Attributes[pubDatabaseAttr]
		if !ok {
			return fmt.Errorf("No Attribute for database is set")
		}

		pubName, ok := rs.Primary.Attributes[pubNameAttr]
		if !ok {
			return fmt.Errorf("No Attribute for publication name is set")
		}

		client := testAccProvider.Meta().(*Client)
		txn, err := startTransaction(client, database)
		if err != nil {
			return err
		}
		defer deferredRollback(txn)

		exists, err := checkPublicationExists(txn, pubName)

		if err != nil {
			return fmt.Errorf("Error checking publication %s", err)
		}

		if !exists {
			return fmt.Errorf("Publication not found")
		}

		return nil
	}
}

func TestAccPostgresqlPublication_Database(t *testing.T) {
	skipIfNotAcc(t)

	dbSuffix, teardown := setupTestDatabase(t, true, true)
	defer teardown()

	dbName, _ := getTestDBNames(dbSuffix)

	testAccPostgresqlPublicationDatabaseConfig := fmt.Sprintf(`
	resource "postgresql_publication" "test" {
		name     = "publication"
		database = "%s"
		owner = "test"
	}
	`, dbName)

	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccPreCheck(t)
			testSuperuserPreCheck(t)
		},
		Providers:    testAccProviders,
		CheckDestroy: testAccCheckPostgresqlPublicationDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccPostgresqlPublicationDatabaseConfig,
				Check: resource.ComposeTestCheckFunc(
					testAccCheckPostgresqlPublicationExists("postgresql_publication.test"),
					resource.TestCheckResourceAttr(
						"postgresql_publication.test", "name", "publication"),
					resource.TestCheckResourceAttr(
						"postgresql_publication.test", "owner", "test"),
					resource.TestCheckResourceAttr(
						"postgresql_publication.test", "database", dbName),
				),
			},
		},
	})
}

func TestAccPostgresqlPublication_UpdateOwner(t *testing.T) {
	skipIfNotAcc(t)

	dbSuffix, teardown := setupTestDatabase(t, true, true)
	defer teardown()

	dbName, _ := getTestDBNames(dbSuffix)
	testOwner := "test_owner"

	testAccPostgresqlPublicationBaseConfig := fmt.Sprintf(`
	resource "postgresql_publication" "test" {
		name     = "publication"
		database = "%s"
	}
	`, dbName)

	testAccPostgresqlPublicationUpdateOwnerConfig := fmt.Sprintf(`
	resource "postgresql_role" "test_owner_2" {
		name = "%s_2"
		login = true
	}
	resource "postgresql_publication" "test" {
		name     = "publication"
		database = "%s"
		owner = "${postgresql_role.test_owner_2.name}"
	}
	`, testOwner, dbName)

	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccPreCheck(t)
			testSuperuserPreCheck(t)
		},
		Providers:    testAccProviders,
		CheckDestroy: testAccCheckPostgresqlPublicationDestroy,
		Steps: []resource.TestStep{
			{
				Config:  testAccPostgresqlPublicationBaseConfig,
				Destroy: false,
				Check: resource.ComposeTestCheckFunc(
					testAccCheckPostgresqlPublicationExists("postgresql_publication.test"),
					resource.TestCheckResourceAttr(
						"postgresql_publication.test", "owner", "postgres"),
					resource.TestCheckResourceAttr(
						"postgresql_publication.test", "name", "publication"),
					resource.TestCheckResourceAttr(
						"postgresql_publication.test", "database", dbName),
				),
			},
			{
				Config: testAccPostgresqlPublicationUpdateOwnerConfig,
				Check: resource.ComposeTestCheckFunc(
					testAccCheckPostgresqlPublicationExists("postgresql_publication.test"),
					resource.TestCheckResourceAttr(
						"postgresql_role.test_owner_2", "name", fmt.Sprintf("%s_2", testOwner)),
					resource.TestCheckResourceAttr(
						"postgresql_publication.test", "name", "publication"),
					resource.TestCheckResourceAttr(
						"postgresql_publication.test", "owner", fmt.Sprintf("%s_2", testOwner)),
					resource.TestCheckResourceAttr(
						"postgresql_publication.test", "database", dbName),
				),
			},
		},
	})
}

func TestAccPostgresqlPublication_UpdateName(t *testing.T) {
	skipIfNotAcc(t)

	dbSuffix, teardown := setupTestDatabase(t, true, true)
	defer teardown()

	dbName, _ := getTestDBNames(dbSuffix)

	testAccPostgresqlPublicationBaseConfig := fmt.Sprintf(`
	resource "postgresql_publication" "test" {
		name     = "%s_publication_1"
		database = "%s"
	}
	`, dbName, dbName)

	testAccPostgresqlPublicationUpdateNameConfig := fmt.Sprintf(`
	resource "postgresql_publication" "test" {
		name     = "%s_publication_2"
		database = "%s"
	}
	`, dbName, dbName)

	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccPreCheck(t)
			testSuperuserPreCheck(t)
		},
		Providers:    testAccProviders,
		CheckDestroy: testAccCheckPostgresqlPublicationDestroy,

		Steps: []resource.TestStep{
			{
				Config: testAccPostgresqlPublicationBaseConfig,
				Check: resource.ComposeTestCheckFunc(
					testAccCheckPostgresqlPublicationExists("postgresql_publication.test"),
					resource.TestCheckResourceAttr(
						"postgresql_publication.test", "name", fmt.Sprintf("%s_publication_1", dbName)),
					resource.TestCheckResourceAttr(
						"postgresql_publication.test", "database", dbName),
				),
			},
			{
				Config: testAccPostgresqlPublicationUpdateNameConfig,
				Check: resource.ComposeTestCheckFunc(
					testAccCheckPostgresqlPublicationExists("postgresql_publication.test"),
					resource.TestCheckResourceAttr(
						"postgresql_publication.test", "name", fmt.Sprintf("%s_publication_2", dbName)),
					resource.TestCheckResourceAttr(
						"postgresql_publication.test", "database", dbName),
				),
			},
			{
				Config: testAccPostgresqlPublicationBaseConfig,
				Check: resource.ComposeTestCheckFunc(
					testAccCheckPostgresqlPublicationExists("postgresql_publication.test"),
					resource.TestCheckResourceAttr(
						"postgresql_publication.test", "name", fmt.Sprintf("%s_publication_1", dbName)),
					resource.TestCheckResourceAttr(
						"postgresql_publication.test", "database", dbName),
				),
			},
		},
	})
}

func checkPublicationExists(txn *sql.Tx, pubName string) (bool, error) {
	var _rez bool
	err := txn.QueryRow("SELECT TRUE from pg_catalog.pg_publication WHERE pubname=$1", pubName).Scan(&_rez)
	switch {
	case err == sql.ErrNoRows:
		return false, nil
	case err != nil:
		return false, fmt.Errorf("Error reading info about publication: %s", err)
	}

	return true, nil
}
