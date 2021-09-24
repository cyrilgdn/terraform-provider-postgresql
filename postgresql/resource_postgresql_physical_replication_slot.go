package postgresql

import (
	"database/sql"
	"fmt"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
)

func resourcePostgreSQLPhysicalReplicationSlot() *schema.Resource {
	return &schema.Resource{
		Create: PGResourceFunc(resourcePostgreSQLPhysicalReplicationSlotCreate),
		Read:   PGResourceFunc(resourcePostgreSQLPhysicalReplicationSlotRead),
		Delete: PGResourceFunc(resourcePostgreSQLPhysicalReplicationSlotDelete),
		Exists: PGResourceExistsFunc(resourcePostgreSQLPhysicalReplicationSlotExists),
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

func resourcePostgreSQLPhysicalReplicationSlotExists(db *DBConnection, d *schema.ResourceData) (bool, error) {
	query := "SELECT 1 FROM pg_catalog.pg_replication_slots WHERE slot_name = $1 and slot_type = 'physical'"
	var unused int
	err := db.QueryRow(query, d.Id()).Scan(&unused)
	switch {
	case err == sql.ErrNoRows:
		return false, nil
	case err != nil:
		return false, err
	}

	return true, nil
}

func resourcePostgreSQLPhysicalReplicationSlotRead(db *DBConnection, d *schema.ResourceData) error {
	d.Set("name", d.Id())
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
