package postgresql

import (
	"database/sql"
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/terraform"
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
				),
			},
		},
	})
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
			return fmt.Errorf("Error checking replication slot %s", err)
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
			return fmt.Errorf("Error checking replication slot %s", err)
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
		return false, fmt.Errorf("Error reading info about replication slot: %s", err)
	}

	return true, nil
}
