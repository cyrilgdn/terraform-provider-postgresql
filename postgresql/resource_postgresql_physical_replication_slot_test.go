package postgresql

import (
	"database/sql"
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
)

func TestAccPostgresqlPhysicalReplicationSlot_Basic(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccPreCheck(t)
			testSuperuserPreCheck(t)
		},
		Providers:    testAccProviders,
		CheckDestroy: testAccCheckPostgresqlPhysicalReplicationSlotDestroy,
		Steps: []resource.TestStep{
			{
				Config: `
				resource "postgresql_physical_replication_slot" "myslot" {
					name = "physical_slot"
				}`,
				Check: resource.ComposeTestCheckFunc(
					testAccCheckPostgresqlPhysicalReplicationSlotExists("postgresql_physical_replication_slot.myslot"),
					resource.TestCheckResourceAttr(
						"postgresql_physical_replication_slot.myslot", "name", "physical_slot"),
				),
			},
		},
	})
}

// TestAccPostgresqlPhysicalReplicationSlot_Disappears ensures that a slot dropped
// outside of Terraform is detected on the next refresh: Read must clear the ID so
// the slot is planned for recreation (non-empty plan). This guards the Read-based
// drift detection that replaced the removed Exists callback.
func TestAccPostgresqlPhysicalReplicationSlot_Disappears(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccPreCheck(t)
			testSuperuserPreCheck(t)
		},
		Providers:    testAccProviders,
		CheckDestroy: testAccCheckPostgresqlPhysicalReplicationSlotDestroy,
		Steps: []resource.TestStep{
			{
				Config: `
				resource "postgresql_physical_replication_slot" "myslot" {
					name = "physical_slot"
				}`,
				Check: resource.ComposeTestCheckFunc(
					testAccCheckPostgresqlPhysicalReplicationSlotExists("postgresql_physical_replication_slot.myslot"),
					testAccDropPhysicalReplicationSlot("physical_slot"),
				),
				ExpectNonEmptyPlan: true,
			},
		},
	})
}

func testAccCheckPostgresqlPhysicalReplicationSlotDestroy(s *terraform.State) error {
	client := testAccProvider.Meta().(*Client)

	for _, rs := range s.RootModule().Resources {
		if rs.Type != "postgresql_physical_replication_slot" {
			continue
		}

		exists, err := checkPhysicalReplicationSlotExists(client, rs.Primary.ID)
		if err != nil {
			return fmt.Errorf("error checking physical replication slot %s", err)
		}

		if exists {
			return fmt.Errorf("PhysicalReplicationSlot still exists after destroy")
		}
	}

	return nil
}

func testAccCheckPostgresqlPhysicalReplicationSlotExists(n string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[n]
		if !ok {
			return fmt.Errorf("Resource not found: %s", n)
		}

		if rs.Primary.ID == "" {
			return fmt.Errorf("No ID is set")
		}

		client := testAccProvider.Meta().(*Client)
		exists, err := checkPhysicalReplicationSlotExists(client, rs.Primary.ID)
		if err != nil {
			return fmt.Errorf("error checking physical replication slot %s", err)
		}

		if !exists {
			return fmt.Errorf("PhysicalReplicationSlot not found")
		}

		return nil
	}
}

// testAccDropPhysicalReplicationSlot drops the slot directly, simulating an
// out-of-band deletion (e.g. by an operator or another tool).
func testAccDropPhysicalReplicationSlot(slotName string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		client := testAccProvider.Meta().(*Client)
		db, err := client.Connect()
		if err != nil {
			return err
		}

		if _, err := db.Exec("SELECT pg_drop_replication_slot($1)", slotName); err != nil {
			return fmt.Errorf("could not drop physical replication slot out-of-band: %w", err)
		}

		return nil
	}
}

func checkPhysicalReplicationSlotExists(client *Client, slotName string) (bool, error) {
	db, err := client.Connect()
	if err != nil {
		return false, err
	}

	var _rez bool
	err = db.QueryRow(
		"SELECT TRUE FROM pg_catalog.pg_replication_slots WHERE slot_name = $1 AND slot_type = 'physical'",
		slotName,
	).Scan(&_rez)
	switch {
	case err == sql.ErrNoRows:
		return false, nil
	case err != nil:
		return false, fmt.Errorf("error reading info about physical replication slot: %s", err)
	}

	return true, nil
}
