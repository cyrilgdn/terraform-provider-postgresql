package postgresql

import (
	"database/sql"
	"fmt"
	"log"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
)

func resourcePostgreSQLPhysicalReplicationSlot() *schema.Resource {
	return &schema.Resource{
		Create: PGResourceFunc(resourcePostgreSQLPhysicalReplicationSlotCreate),
		Read:   PGResourceFunc(resourcePostgreSQLPhysicalReplicationSlotRead),
		Delete: PGResourceFunc(resourcePostgreSQLPhysicalReplicationSlotDelete),
		Importer: &schema.ResourceImporter{
			StateContext: schema.ImportStatePassthroughContext,
		},

		Schema: map[string]*schema.Schema{
			"name": {
				Type:     schema.TypeString,
				Required: true,
				ForceNew: true,
			},
		},
	}
}

func resourcePostgreSQLPhysicalReplicationSlotCreate(db *DBConnection, d *schema.ResourceData) error {
	name := d.Get("name").(string)
	sql := "SELECT FROM pg_create_physical_replication_slot($1)"
	if _, err := db.Exec(sql, name); err != nil {
		return fmt.Errorf("could not create physical ReplicationSlot %s: %w", name, err)
	}
	d.SetId(name)

	return nil
}

func resourcePostgreSQLPhysicalReplicationSlotRead(db *DBConnection, d *schema.ResourceData) error {
	var slotName string
	query := "SELECT slot_name FROM pg_catalog.pg_replication_slots WHERE slot_name = $1 AND slot_type = 'physical'"
	err := db.QueryRow(query, d.Id()).Scan(&slotName)
	switch {
	case err == sql.ErrNoRows:
		log.Printf("[WARN] PostgreSQL physical replication slot (%s) not found", d.Id())
		d.SetId("")
		return nil
	case err != nil:
		return fmt.Errorf("error reading physical replication slot: %w", err)
	}

	d.Set("name", slotName)
	return nil
}

func resourcePostgreSQLPhysicalReplicationSlotDelete(db *DBConnection, d *schema.ResourceData) error {

	replicationSlotName := d.Get("name").(string)

	if _, err := db.Exec("SELECT pg_drop_replication_slot($1)", replicationSlotName); err != nil {
		return err
	}

	d.SetId("")
	return nil
}
