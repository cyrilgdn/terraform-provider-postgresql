package postgresql

import (
	"database/sql"
	"fmt"
	"log"
	"strings"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
)

func resourcePostgreSQLReplicationSlot() *schema.Resource {
	return &schema.Resource{
		Create: PGResourceFunc(resourcePostgreSQLReplicationSlotCreate),
		Read:   PGResourceFunc(resourcePostgreSQLReplicationSlotRead),
		Delete: PGResourceFunc(resourcePostgreSQLReplicationSlotDelete),
		Exists: PGResourceExistsFunc(resourcePostgreSQLReplicationSlotExists),
		Importer: &schema.ResourceImporter{
			StateContext: schema.ImportStatePassthroughContext,
		},

		Schema: map[string]*schema.Schema{
			"name": {
				Type:     schema.TypeString,
				Required: true,
				ForceNew: true,
			},
			"database": {
				Type:        schema.TypeString,
				Optional:    true,
				Computed:    true,
				ForceNew:    true,
				Description: "Sets the database to add the replication slot to",
			},
			"plugin": {
				Type:        schema.TypeString,
				Required:    true,
				ForceNew:    true,
				Description: "Sets the output plugin to use",
			},
		},
	}
}

func resourcePostgreSQLReplicationSlotCreate(db *DBConnection, d *schema.ResourceData) error {

	name := d.Get("name").(string)
	plugin := d.Get("plugin").(string)
	databaseName := getDatabaseForReplicationSlot(d, db.client.databaseName)

	txn, err := startTransaction(db.client, databaseName)
	if err != nil {
		return err
	}
	defer deferredRollback(txn)

	sql := "SELECT FROM pg_create_logical_replication_slot($1, $2)"
	if _, err := txn.Exec(sql, name, plugin); err != nil {
		return err
	}

	if err = txn.Commit(); err != nil {
		return fmt.Errorf("error creating ReplicationSlot: %w", err)
	}

	d.SetId(generateReplicationSlotID(d, databaseName))

	return resourcePostgreSQLReplicationSlotReadImpl(db, d)
}

func resourcePostgreSQLReplicationSlotExists(db *DBConnection, d *schema.ResourceData) (bool, error) {

	var ReplicationSlotName string

	database, replicationSlotName, err := getDBReplicationSlotName(d, db.client)
	if err != nil {
		return false, err
	}

	// Check if the database exists
	exists, err := dbExists(db, database)
	if err != nil || !exists {
		return false, err
	}

	txn, err := startTransaction(db.client, database)
	if err != nil {
		return false, err
	}
	defer deferredRollback(txn)

	query := "SELECT slot_name FROM pg_catalog.pg_replication_slots WHERE slot_name = $1 and database = $2"
	err = txn.QueryRow(query, replicationSlotName, database).Scan(&ReplicationSlotName)
	switch {
	case err == sql.ErrNoRows:
		return false, nil
	case err != nil:
		return false, err
	}

	return true, nil
}

func resourcePostgreSQLReplicationSlotRead(db *DBConnection, d *schema.ResourceData) error {
	return resourcePostgreSQLReplicationSlotReadImpl(db, d)
}

func resourcePostgreSQLReplicationSlotReadImpl(db *DBConnection, d *schema.ResourceData) error {
	database, replicationSlotName, err := getDBReplicationSlotName(d, db.client)
	if err != nil {
		return err
	}

	txn, err := startTransaction(db.client, database)
	if err != nil {
		return err
	}
	defer deferredRollback(txn)

	var replicationSlotPlugin string
	query := `SELECT plugin ` +
		`FROM pg_catalog.pg_replication_slots ` +
		`WHERE slot_name = $1 AND database = $2`
	err = txn.QueryRow(query, replicationSlotName, database).Scan(&replicationSlotPlugin)
	switch {
	case err == sql.ErrNoRows:
		log.Printf("[WARN] PostgreSQL ReplicationSlot (%s) not found for database %s", replicationSlotName, database)
		d.SetId("")
		return nil
	case err != nil:
		return fmt.Errorf("error reading ReplicationSlot: %w", err)
	}

	d.Set("name", replicationSlotName)
	d.Set("plugin", replicationSlotPlugin)
	d.Set("database", database)
	d.SetId(generateReplicationSlotID(d, database))

	return nil
}

func resourcePostgreSQLReplicationSlotDelete(db *DBConnection, d *schema.ResourceData) error {

	replicationSlotName := d.Get("name").(string)
	database := getDatabaseForReplicationSlot(d, db.client.databaseName)

	txn, err := startTransaction(db.client, database)
	if err != nil {
		return err
	}
	defer deferredRollback(txn)

	sql := "SELECT pg_drop_replication_slot($1)"
	if _, err := txn.Exec(sql, replicationSlotName); err != nil {
		return err
	}

	if err = txn.Commit(); err != nil {
		return fmt.Errorf("error deleting ReplicationSlot: %w", err)
	}

	d.SetId("")

	return nil
}

func getDatabaseForReplicationSlot(d *schema.ResourceData, databaseName string) string {
	if v, ok := d.GetOk("database"); ok {
		databaseName = v.(string)
	}

	return databaseName
}

func generateReplicationSlotID(d *schema.ResourceData, databaseName string) string {
	return strings.Join([]string{
		databaseName,
		d.Get("name").(string),
	}, ".")
}

func getReplicationSlotNameFromID(ID string) string {
	splitted := strings.Split(ID, ".")
	return splitted[0]
}

// getDBReplicationSlotName returns database and replication slot name. If we are importing this
// resource, they will be parsed from the resource ID (it will return an error if parsing failed)
// otherwise they will be simply get from the state.
func getDBReplicationSlotName(d *schema.ResourceData, client *Client) (string, string, error) {
	database := getDatabaseForReplicationSlot(d, client.databaseName)
	replicationSlotName := d.Get("name").(string)

	// When importing, we have to parse the ID to find replication slot and database names.
	if replicationSlotName == "" {
		parsed := strings.Split(d.Id(), ".")
		if len(parsed) != 2 {
			return "", "", fmt.Errorf("replication Slot ID %s has not the expected format 'database.replication_slot': %v", d.Id(), parsed)
		}
		database = parsed[0]
		replicationSlotName = parsed[1]
	}
	return database, replicationSlotName, nil
}
