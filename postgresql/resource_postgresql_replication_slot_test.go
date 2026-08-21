package postgresql

import (
	"database/sql"
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
)

func TestAccPostgresqlReplicationSlot_Basic(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccPreCheck(t)
			testSuperuserPreCheck(t)
		},
		Providers:    testAccProviders,
		CheckDestroy: testAccCheckPostgresqlReplicationSlotDestroy,
		Steps: []resource.TestStep{
			{
				Config: `
				resource "postgresql_replication_slot" "myslot" {
					name   = "slot"
					plugin = "test_decoding"
				}`,
				Check: resource.ComposeTestCheckFunc(
					testAccCheckPostgresqlReplicationSlotExists("postgresql_replication_slot.myslot"),
					resource.TestCheckResourceAttr(
						"postgresql_replication_slot.myslot", "name", "slot"),
					resource.TestCheckResourceAttr(
						"postgresql_replication_slot.myslot", "plugin", "test_decoding"),
					resource.TestCheckResourceAttr(
						"postgresql_replication_slot.myslot", "failover", "false"),
				),
			},
		},
	})
}

func TestAccPostgresqlReplicationSlot_Failover(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccPreCheck(t)
			testSuperuserPreCheck(t)
			testCheckReplicationSlotFailoverSupported(t)
		},
		Providers:    testAccProviders,
		CheckDestroy: testAccCheckPostgresqlReplicationSlotDestroy,
		Steps: []resource.TestStep{
			{
				Config: `
				resource "postgresql_replication_slot" "myslot" {
					name     = "slot_failover"
					plugin   = "test_decoding"
					failover = true
				}`,
				Check: resource.ComposeTestCheckFunc(
					testAccCheckPostgresqlReplicationSlotExists("postgresql_replication_slot.myslot"),
					resource.TestCheckResourceAttr(
						"postgresql_replication_slot.myslot", "failover", "true"),
				),
			},
		},
	})
}

// testCheckReplicationSlotFailoverSupported skips the test when connected to a
// PostgreSQL server older than 17, since failover on replication slots doesn't exist
// there (pg_create_logical_replication_slot only grew the parameter in PG 17).
func testCheckReplicationSlotFailoverSupported(t *testing.T) {
	client := testAccProvider.Meta().(*Client)
	db, err := client.Connect()
	if err != nil {
		t.Fatalf("could not connect to check PostgreSQL version: %v", err)
	}
	if !db.featureSupported(featureReplicationSlotFailover) {
		t.Skipf("skipping, failover on replication slots requires PostgreSQL >= 17 (connected to %s)", db.version)
	}
}

func testAccCheckPostgresqlReplicationSlotDestroy(s *terraform.State) error {
	client := testAccProvider.Meta().(*Client)

	for _, rs := range s.RootModule().Resources {
		if rs.Type != "postgresql_replication_slot" {
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

		exists, err := checkReplicationSlotExists(txn, getReplicationSlotNameFromID(rs.Primary.ID))

		if err != nil {
			return fmt.Errorf("error checking replication slot %s", err)
		}

		if exists {
			return fmt.Errorf("ReplicationSlot still exists after destroy")
		}
	}

	return nil
}

func testAccCheckPostgresqlReplicationSlotExists(n string) resource.TestCheckFunc {
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
			return fmt.Errorf("No Attribute for replication slot name is set")
		}

		client := testAccProvider.Meta().(*Client)
		txn, err := startTransaction(client, database)
		if err != nil {
			return err
		}
		defer deferredRollback(txn)

		exists, err := checkReplicationSlotExists(txn, extName)

		if err != nil {
			return fmt.Errorf("error checking replication slot %s", err)
		}

		if !exists {
			return fmt.Errorf("ReplicationSlot not found")
		}

		return nil
	}
}

func TestAccPostgresqlReplicationSlot_Database(t *testing.T) {
	skipIfNotAcc(t)

	dbSuffix, teardown := setupTestDatabase(t, true, true)
	defer teardown()

	dbName, _ := getTestDBNames(dbSuffix)

	testAccPostgresqlReplicationSlotDatabaseConfig := fmt.Sprintf(`
	resource "postgresql_replication_slot" "myslot" {
		name     = "slot"
		plugin   = "test_decoding"
		database = "%s"
	}
	`, dbName)

	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccPreCheck(t)
			testSuperuserPreCheck(t)
		},
		Providers:    testAccProviders,
		CheckDestroy: testAccCheckPostgresqlReplicationSlotDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccPostgresqlReplicationSlotDatabaseConfig,
				Check: resource.ComposeTestCheckFunc(
					testAccCheckPostgresqlReplicationSlotExists("postgresql_replication_slot.myslot"),
					resource.TestCheckResourceAttr(
						"postgresql_replication_slot.myslot", "name", "slot"),
					resource.TestCheckResourceAttr(
						"postgresql_replication_slot.myslot", "plugin", "test_decoding"),
					resource.TestCheckResourceAttr(
						"postgresql_replication_slot.myslot", "database", dbName),
				),
			},
		},
	})
}

func checkReplicationSlotExists(txn *sql.Tx, slotName string) (bool, error) {
	var _rez bool
	err := txn.QueryRow("SELECT TRUE from pg_catalog.pg_replication_slots d WHERE slot_name=$1", slotName).Scan(&_rez)
	switch {
	case err == sql.ErrNoRows:
		return false, nil
	case err != nil:
		return false, fmt.Errorf("error reading info about replication slot: %s", err)
	}

	return true, nil
}
