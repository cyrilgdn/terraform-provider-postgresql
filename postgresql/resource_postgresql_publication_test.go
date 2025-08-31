package postgresql

import (
	"database/sql"
	"fmt"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
)

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
			return fmt.Errorf("error checking publication %s", err)
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
			return fmt.Errorf("error checking publication %s", err)
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
	resource "postgresql_role" "test" {
		name = "test"
	}
	resource "postgresql_publication" "test" {
		name     = "publication"
		database = "%s"
		owner = postgresql_role.test.name
	}
	`, dbName)

	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccPreCheck(t)
			testCheckCompatibleVersion(t, featurePublication)
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

func TestAccPostgresqlPublication_UpdateTables(t *testing.T) {
	skipIfNotAcc(t)

	dbSuffix, teardown := setupTestDatabase(t, true, true)
	defer teardown()
	testTables := []string{"test_schema.test_table_1", "test_schema.test_table_2", "test_schema.test_table_3"}
	createTestTables(t, dbSuffix, testTables, "")

	dbName, _ := getTestDBNames(dbSuffix)

	testAccPostgresqlPublicationBaseConfig := fmt.Sprintf(`
	resource "postgresql_publication" "test" {
		name     = "publication"
		database = "%s"
		tables = ["test_schema.test_table_1", "test_schema.test_table_2"]
	}
	`, dbName)

	testAccPostgresqlPublicationUpdateTablesConfig := fmt.Sprintf(`
	resource "postgresql_publication" "test" {
		name     = "publication"
		database = "%s"
		tables = ["test_schema.test_table_1", "test_schema.test_table_3"]
	}
	`, dbName)

	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccPreCheck(t)
			testCheckCompatibleVersion(t, featurePublication)
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
						"postgresql_publication.test", "name", "publication"),
					resource.TestCheckResourceAttr(
						"postgresql_publication.test", pubDatabaseAttr, dbName),
					resource.TestCheckResourceAttr(
						"postgresql_publication.test", pubAllTablesAttr, "false"),
					resource.TestCheckResourceAttr(
						"postgresql_publication.test", fmt.Sprintf("%s.0", pubTablesAttr), "test_schema.test_table_1"),
					resource.TestCheckResourceAttr(
						"postgresql_publication.test", fmt.Sprintf("%s.1", pubTablesAttr), "test_schema.test_table_2"),
				),
			},
			{
				Config: testAccPostgresqlPublicationUpdateTablesConfig,
				Check: resource.ComposeTestCheckFunc(
					testAccCheckPostgresqlPublicationExists("postgresql_publication.test"),
					resource.TestCheckResourceAttr(
						"postgresql_publication.test", "name", "publication"),
					resource.TestCheckResourceAttr(
						"postgresql_publication.test", "database", dbName),
					resource.TestCheckResourceAttr(
						"postgresql_publication.test", pubAllTablesAttr, "false"),
					resource.TestCheckResourceAttr(
						"postgresql_publication.test", fmt.Sprintf("%s.0", pubTablesAttr), "test_schema.test_table_1"),
					resource.TestCheckResourceAttr(
						"postgresql_publication.test", fmt.Sprintf("%s.1", pubTablesAttr), "test_schema.test_table_3"),
				),
			},
		},
	})
}

func TestAccPostgresqlPublication_UpdatePublishParams(t *testing.T) {
	skipIfNotAcc(t)

	dbSuffix, teardown := setupTestDatabase(t, true, true)
	defer teardown()
	testTables := []string{"test_schema.test_table_1", "test_schema.test_table_2", "test_schema.test_table_3"}
	createTestTables(t, dbSuffix, testTables, "")

	dbName, _ := getTestDBNames(dbSuffix)

	testAccPostgresqlPublicationBaseConfig := fmt.Sprintf(`
	resource "postgresql_publication" "test" {
		name     = "publication"
		database = "%s"
	}
	`, dbName)

	testAccPostgresqlPublicationUpdateParamsConfig := fmt.Sprintf(`
	resource "postgresql_publication" "test" {
		name     = "publication"
		database = "%s"
		publish_param = ["update", "truncate"]
	}
	`, dbName)

	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccPreCheck(t)
			testCheckCompatibleVersion(t, featurePublication)
			testCheckCompatibleVersion(t, featurePubTruncate)
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
						"postgresql_publication.test", "name", "publication"),
					resource.TestCheckResourceAttr(
						"postgresql_publication.test", pubDatabaseAttr, dbName),
					resource.TestCheckResourceAttr(
						"postgresql_publication.test", fmt.Sprintf("%s.#", pubPublishAttr), "4"),
					resource.TestCheckResourceAttr(
						"postgresql_publication.test", fmt.Sprintf("%s.0", pubPublishAttr), "insert"),
					resource.TestCheckResourceAttr(
						"postgresql_publication.test", fmt.Sprintf("%s.1", pubPublishAttr), "update"),
					resource.TestCheckResourceAttr(
						"postgresql_publication.test", fmt.Sprintf("%s.2", pubPublishAttr), "delete"),
					resource.TestCheckResourceAttr(
						"postgresql_publication.test", fmt.Sprintf("%s.3", pubPublishAttr), "truncate"),
				),
			},
			{
				Config: testAccPostgresqlPublicationUpdateParamsConfig,
				Check: resource.ComposeTestCheckFunc(
					testAccCheckPostgresqlPublicationExists("postgresql_publication.test"),
					resource.TestCheckResourceAttr(
						"postgresql_publication.test", "name", "publication"),
					resource.TestCheckResourceAttr(
						"postgresql_publication.test", pubDatabaseAttr, dbName),
					resource.TestCheckResourceAttr(
						"postgresql_publication.test", fmt.Sprintf("%s.#", pubPublishAttr), "2"),
					resource.TestCheckResourceAttr(
						"postgresql_publication.test", fmt.Sprintf("%s.0", pubPublishAttr), "update"),
					resource.TestCheckResourceAttr(
						"postgresql_publication.test", fmt.Sprintf("%s.1", pubPublishAttr), "truncate"),
				),
			},
		},
	})
}

func TestAccPostgresqlPublication_UpdatePublishParamsWithoutTruncate(t *testing.T) {
	skipIfNotAcc(t)

	dbSuffix, teardown := setupTestDatabase(t, true, true)
	defer teardown()
	testTables := []string{"test_schema.test_table_1", "test_schema.test_table_2", "test_schema.test_table_3"}
	createTestTables(t, dbSuffix, testTables, "")

	dbName, _ := getTestDBNames(dbSuffix)

	testAccPostgresqlPublicationBaseConfig := fmt.Sprintf(`
	resource "postgresql_publication" "test" {
		name     = "publication"
		database = "%s"
	}
	`, dbName)

	testAccPostgresqlPublicationUpdateParamsConfig := fmt.Sprintf(`
	resource "postgresql_publication" "test" {
		name     = "publication"
		database = "%s"
		publish_param = ["update"]
	}
	`, dbName)

	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccPreCheck(t)
			testCheckCompatibleVersion(t, featurePublication)
			testCheckCompatibleVersion(t, featurePubWithoutTruncate)
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
						"postgresql_publication.test", "name", "publication"),
					resource.TestCheckResourceAttr(
						"postgresql_publication.test", pubDatabaseAttr, dbName),
					resource.TestCheckResourceAttr(
						"postgresql_publication.test", fmt.Sprintf("%s.#", pubPublishAttr), "3"),
					resource.TestCheckResourceAttr(
						"postgresql_publication.test", fmt.Sprintf("%s.0", pubPublishAttr), "insert"),
					resource.TestCheckResourceAttr(
						"postgresql_publication.test", fmt.Sprintf("%s.1", pubPublishAttr), "update"),
					resource.TestCheckResourceAttr(
						"postgresql_publication.test", fmt.Sprintf("%s.2", pubPublishAttr), "delete"),
				),
			},
			{
				Config: testAccPostgresqlPublicationUpdateParamsConfig,
				Check: resource.ComposeTestCheckFunc(
					testAccCheckPostgresqlPublicationExists("postgresql_publication.test"),
					resource.TestCheckResourceAttr(
						"postgresql_publication.test", "name", "publication"),
					resource.TestCheckResourceAttr(
						"postgresql_publication.test", pubDatabaseAttr, dbName),
					resource.TestCheckResourceAttr(
						"postgresql_publication.test", fmt.Sprintf("%s.#", pubPublishAttr), "1"),
					resource.TestCheckResourceAttr(
						"postgresql_publication.test", fmt.Sprintf("%s.0", pubPublishAttr), "update"),
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
			testCheckCompatibleVersion(t, featurePublication)
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
			testCheckCompatibleVersion(t, featurePublication)
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
		return false, fmt.Errorf("error reading info about publication: %s", err)
	}

	return true, nil
}

func TestAccPostgresqlPublication_Basic(t *testing.T) {
	skipIfNotAcc(t)

	dbSuffix, teardown := setupTestDatabase(t, true, true)
	defer teardown()
	testTables := []string{"test_schema.test_table_1", "test_schema.test_table_2", "test_schema.test_table_3"}
	createTestTables(t, dbSuffix, testTables, "")

	dbName, _ := getTestDBNames(dbSuffix)
	testAccPostgresqlPublicationBasicConfig := fmt.Sprintf(`
resource "postgresql_role" "test_owner" {
	name = "test_owner"
	login = true
}

resource "postgresql_publication" "test" {
	name     = "publication"
	database = "%s"
	owner = "${postgresql_role.test_owner.name}"
	all_tables = true
}
`, dbName)

	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccPreCheck(t)
			testCheckCompatibleVersion(t, featurePublication)
			testCheckCompatibleVersion(t, featurePubTruncate)
			testSuperuserPreCheck(t)
		},
		Providers:    testAccProviders,
		CheckDestroy: testAccCheckPostgresqlPublicationDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccPostgresqlPublicationBasicConfig,
				Check: resource.ComposeTestCheckFunc(
					testAccCheckPostgresqlPublicationExists("postgresql_publication.test"),
					resource.TestCheckResourceAttr(
						"postgresql_publication.test", "name", "publication"),
					resource.TestCheckResourceAttr(
						"postgresql_publication.test", pubDatabaseAttr, dbName),
					resource.TestCheckResourceAttr(
						"postgresql_publication.test", pubAllTablesAttr, "true"),
					resource.TestCheckResourceAttr(
						"postgresql_publication.test", pubOwnerAttr, "test_owner"),
					resource.TestCheckResourceAttr(
						"postgresql_publication.test", fmt.Sprintf("%s.#", pubTablesAttr), "3"),
					resource.TestCheckResourceAttr(
						"postgresql_publication.test", fmt.Sprintf("%s.0", pubTablesAttr), "test_schema.test_table_1"),
					resource.TestCheckResourceAttr(
						"postgresql_publication.test", fmt.Sprintf("%s.1", pubTablesAttr), "test_schema.test_table_2"),
					resource.TestCheckResourceAttr(
						"postgresql_publication.test", fmt.Sprintf("%s.2", pubTablesAttr), "test_schema.test_table_3"),
					resource.TestCheckResourceAttr(
						"postgresql_publication.test", fmt.Sprintf("%s.#", pubPublishAttr), "4"),
					resource.TestCheckResourceAttr(
						"postgresql_publication.test", fmt.Sprintf("%s.0", pubPublishAttr), "insert"),
					resource.TestCheckResourceAttr(
						"postgresql_publication.test", fmt.Sprintf("%s.1", pubPublishAttr), "update"),
					resource.TestCheckResourceAttr(
						"postgresql_publication.test", fmt.Sprintf("%s.2", pubPublishAttr), "delete"),
					resource.TestCheckResourceAttr(
						"postgresql_publication.test", fmt.Sprintf("%s.3", pubPublishAttr), "truncate"),
				),
			},
		},
	})
}

func TestAccPostgresqlPublication_BasicWithoutTruncate(t *testing.T) {
	skipIfNotAcc(t)

	dbSuffix, teardown := setupTestDatabase(t, true, true)
	defer teardown()
	testTables := []string{"test_schema.test_table_1", "test_schema.test_table_2", "test_schema.test_table_3"}
	createTestTables(t, dbSuffix, testTables, "")

	dbName, _ := getTestDBNames(dbSuffix)
	testAccPostgresqlPublicationBasicConfig := fmt.Sprintf(`
resource "postgresql_role" "test_owner" {
	name = "test_owner"
	login = true
}

resource "postgresql_publication" "test" {
	name     = "publication"
	database = "%s"
	owner = "${postgresql_role.test_owner.name}"
	all_tables = true
}
`, dbName)

	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccPreCheck(t)
			testCheckCompatibleVersion(t, featurePublication)
			testCheckCompatibleVersion(t, featurePubWithoutTruncate)
			testSuperuserPreCheck(t)
		},
		Providers:    testAccProviders,
		CheckDestroy: testAccCheckPostgresqlPublicationDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccPostgresqlPublicationBasicConfig,
				Check: resource.ComposeTestCheckFunc(
					testAccCheckPostgresqlPublicationExists("postgresql_publication.test"),
					resource.TestCheckResourceAttr(
						"postgresql_publication.test", "name", "publication"),
					resource.TestCheckResourceAttr(
						"postgresql_publication.test", pubDatabaseAttr, dbName),
					resource.TestCheckResourceAttr(
						"postgresql_publication.test", pubAllTablesAttr, "true"),
					resource.TestCheckResourceAttr(
						"postgresql_publication.test", pubOwnerAttr, "test_owner"),
					resource.TestCheckResourceAttr(
						"postgresql_publication.test", fmt.Sprintf("%s.#", pubTablesAttr), "3"),
					resource.TestCheckResourceAttr(
						"postgresql_publication.test", fmt.Sprintf("%s.0", pubTablesAttr), "test_schema.test_table_1"),
					resource.TestCheckResourceAttr(
						"postgresql_publication.test", fmt.Sprintf("%s.1", pubTablesAttr), "test_schema.test_table_2"),
					resource.TestCheckResourceAttr(
						"postgresql_publication.test", fmt.Sprintf("%s.2", pubTablesAttr), "test_schema.test_table_3"),
					resource.TestCheckResourceAttr(
						"postgresql_publication.test", fmt.Sprintf("%s.#", pubPublishAttr), "3"),
					resource.TestCheckResourceAttr(
						"postgresql_publication.test", fmt.Sprintf("%s.0", pubPublishAttr), "insert"),
					resource.TestCheckResourceAttr(
						"postgresql_publication.test", fmt.Sprintf("%s.1", pubPublishAttr), "update"),
					resource.TestCheckResourceAttr(
						"postgresql_publication.test", fmt.Sprintf("%s.2", pubPublishAttr), "delete"),
				),
			},
		},
	})
}

func TestAccPostgresqlPublication_ConflictTables(t *testing.T) {
	skipIfNotAcc(t)

	dbSuffix, teardown := setupTestDatabase(t, true, true)
	defer teardown()

	dbName, _ := getTestDBNames(dbSuffix)
	testAccPostgresqlPublicationBasicConfig := fmt.Sprintf(`
resource "postgresql_publication" "test" {
	name     = "publication"
	database = "%s"
	tables = ["test.table1","test.table2"]
	all_tables = true
}
`, dbName)

	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccPreCheck(t)
			testCheckCompatibleVersion(t, featurePublication)
			testSuperuserPreCheck(t)
		},
		Providers:    testAccProviders,
		CheckDestroy: testAccCheckPostgresqlPublicationDestroy,
		Steps: []resource.TestStep{
			{
				Config:      testAccPostgresqlPublicationBasicConfig,
				ExpectError: regexp.MustCompile("Conflicting configuration arguments.*"),
			},
		},
	})
}

func TestAccPostgresqlPublication_CheckPublishViaRoot(t *testing.T) {
	skipIfNotAcc(t)

	dbSuffix, teardown := setupTestDatabase(t, true, true)
	defer teardown()

	dbName, _ := getTestDBNames(dbSuffix)
	testAccPostgresqlPublicationBasicConfig := fmt.Sprintf(`
resource "postgresql_publication" "test" {
	name     = "publication"
	database = "%s"
}
`, dbName)

	testAccPostgresqlPublicationBasicUpdateKeysWithPartitionRoot := fmt.Sprintf(`
resource "postgresql_publication" "test" {
	name     = "publication"
	database = "%s"
	publish_param = ["update","delete"]
	publish_via_partition_root_param = true
}
`, dbName)

	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccPreCheck(t)
			testCheckCompatibleVersion(t, featurePublication)
			testCheckCompatibleVersion(t, featurePublishViaRoot)
			testSuperuserPreCheck(t)
		},
		Providers:    testAccProviders,
		CheckDestroy: testAccCheckPostgresqlPublicationDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccPostgresqlPublicationBasicConfig,
				Check: resource.ComposeTestCheckFunc(
					testAccCheckPostgresqlPublicationExists("postgresql_publication.test"),
					resource.TestCheckResourceAttr(
						"postgresql_publication.test", "name", "publication"),
					resource.TestCheckResourceAttr(
						"postgresql_publication.test", pubDatabaseAttr, dbName),
					resource.TestCheckResourceAttr(
						"postgresql_publication.test", pubPublishViaPartitionRootAttr, "false"),
				),
			},
			{
				Config: testAccPostgresqlPublicationBasicUpdateKeysWithPartitionRoot,
				Check: resource.ComposeTestCheckFunc(
					testAccCheckPostgresqlPublicationExists("postgresql_publication.test"),
					resource.TestCheckResourceAttr(
						"postgresql_publication.test", "name", "publication"),
					resource.TestCheckResourceAttr(
						"postgresql_publication.test", pubDatabaseAttr, dbName),
					resource.TestCheckResourceAttr(
						"postgresql_publication.test", fmt.Sprintf("%s.#", pubPublishAttr), "2"),
					resource.TestCheckResourceAttr(
						"postgresql_publication.test", fmt.Sprintf("%s.0", pubPublishAttr), "update"),
					resource.TestCheckResourceAttr(
						"postgresql_publication.test", fmt.Sprintf("%s.1", pubPublishAttr), "delete"),
					resource.TestCheckResourceAttr(
						"postgresql_publication.test", pubPublishViaPartitionRootAttr, "true"),
				),
			},
		},
	})
}

func TestAccPostgresqlPublication_CheckPublishParams(t *testing.T) {
	skipIfNotAcc(t)

	dbSuffix, teardown := setupTestDatabase(t, true, true)
	defer teardown()

	dbName, _ := getTestDBNames(dbSuffix)
	testAccPostgresqlPublicationBasicConfig := fmt.Sprintf(`
resource "postgresql_publication" "test" {
	name     = "publication"
	database = "%s"
	publish_param = ["insert"]
}
`, dbName)
	testAccPostgresqlPublicationWrongKeys := fmt.Sprintf(`
resource "postgresql_publication" "test" {
	name     = "publication"
	database = "%s"
	publish_param = ["insert","wrong_param"]
}
`, dbName)

	testAccPostgresqlPublicationDuplicateKeys := fmt.Sprintf(`
resource "postgresql_publication" "test" {
	name     = "publication"
	database = "%s"
	publish_param = ["insert","insert"]
}
`, dbName)
	testAccPostgresqlPublicationBasicUpdateKeys := fmt.Sprintf(`
resource "postgresql_publication" "test" {
	name     = "publication"
	database = "%s"
	publish_param = ["update","delete"]
}
`, dbName)

	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccPreCheck(t)
			testCheckCompatibleVersion(t, featurePublication)
			testSuperuserPreCheck(t)
		},
		Providers:    testAccProviders,
		CheckDestroy: testAccCheckPostgresqlPublicationDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccPostgresqlPublicationBasicConfig,
				Check: resource.ComposeTestCheckFunc(
					testAccCheckPostgresqlPublicationExists("postgresql_publication.test"),
					resource.TestCheckResourceAttr(
						"postgresql_publication.test", "name", "publication"),
					resource.TestCheckResourceAttr(
						"postgresql_publication.test", pubDatabaseAttr, dbName),
					resource.TestCheckResourceAttr(
						"postgresql_publication.test", fmt.Sprintf("%s.#", pubPublishAttr), "1"),
					resource.TestCheckResourceAttr(
						"postgresql_publication.test", fmt.Sprintf("%s.0", pubPublishAttr), "insert"),
				),
			},
			{
				Config: testAccPostgresqlPublicationBasicUpdateKeys,
				Check: resource.ComposeTestCheckFunc(
					testAccCheckPostgresqlPublicationExists("postgresql_publication.test"),
					resource.TestCheckResourceAttr(
						"postgresql_publication.test", "name", "publication"),
					resource.TestCheckResourceAttr(
						"postgresql_publication.test", pubDatabaseAttr, dbName),
					resource.TestCheckResourceAttr(
						"postgresql_publication.test", fmt.Sprintf("%s.#", pubPublishAttr), "2"),
					resource.TestCheckResourceAttr(
						"postgresql_publication.test", fmt.Sprintf("%s.0", pubPublishAttr), "update"),
					resource.TestCheckResourceAttr(
						"postgresql_publication.test", fmt.Sprintf("%s.1", pubPublishAttr), "delete"),
				),
			},
			{
				Config:      testAccPostgresqlPublicationWrongKeys,
				ExpectError: regexp.MustCompile("could not update publication tables: Error getting publication parameters: invalid value of `publish_param`: wrong_param. Should be at least one of 'insert, update, delete, truncate'"),
			},
			{
				Config:      testAccPostgresqlPublicationDuplicateKeys,
				ExpectError: regexp.MustCompile("'insert' is duplicated for attribute `tables`")},
		},
	})
}
