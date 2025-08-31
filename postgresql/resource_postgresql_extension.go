package postgresql

import (
	"bytes"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"strings"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/lib/pq"
)

const (
	extNameAttr          = "name"
	extSchemaAttr        = "schema"
	extVersionAttr       = "version"
	extDatabaseAttr      = "database"
	extDropCascadeAttr   = "drop_cascade"
	extCreateCascadeAttr = "create_cascade"
)

func resourcePostgreSQLExtension() *schema.Resource {
	return &schema.Resource{
		Create: PGResourceFunc(resourcePostgreSQLExtensionCreate),
		Read:   PGResourceFunc(resourcePostgreSQLExtensionRead),
		Update: PGResourceFunc(resourcePostgreSQLExtensionUpdate),
		Delete: PGResourceFunc(resourcePostgreSQLExtensionDelete),
		Exists: PGResourceExistsFunc(resourcePostgreSQLExtensionExists),
		Importer: &schema.ResourceImporter{
			StateContext: schema.ImportStatePassthroughContext,
		},

		Schema: map[string]*schema.Schema{
			extNameAttr: {
				Type:     schema.TypeString,
				Required: true,
				ForceNew: true,
			},
			extSchemaAttr: {
				Type:        schema.TypeString,
				Optional:    true,
				Computed:    true,
				Description: "Sets the schema of an extension",
			},
			extVersionAttr: {
				Type:        schema.TypeString,
				Optional:    true,
				Computed:    true,
				Description: "Sets the version number of the extension",
			},
			extDatabaseAttr: {
				Type:        schema.TypeString,
				Optional:    true,
				Computed:    true,
				ForceNew:    true,
				Description: "Sets the database to add the extension to",
			},
			extDropCascadeAttr: {
				Type:        schema.TypeBool,
				Optional:    true,
				Default:     false,
				Description: "When true, will also drop all the objects that depend on the extension, and in turn all objects that depend on those objects",
			},
			extCreateCascadeAttr: {
				Type:        schema.TypeBool,
				Optional:    true,
				Default:     false,
				Description: "When true, will also create any extensions that this extension depends on that are not already installed",
			},
		},
	}
}

func resourcePostgreSQLExtensionCreate(db *DBConnection, d *schema.ResourceData) error {
	if !db.featureSupported(featureExtension) {
		return fmt.Errorf(
			"postgresql_extension resource is not supported for this Postgres version (%s)",
			db.version,
		)
	}

	extName := d.Get(extNameAttr).(string)
	databaseName := getDatabaseForExtension(d, db.client.databaseName)

	b := bytes.NewBufferString("CREATE EXTENSION IF NOT EXISTS ")
	fmt.Fprint(b, pq.QuoteIdentifier(extName))

	if v, ok := d.GetOk(extSchemaAttr); ok {
		fmt.Fprint(b, " SCHEMA ", pq.QuoteIdentifier(v.(string)))
	}

	if v, ok := d.GetOk(extVersionAttr); ok {
		fmt.Fprint(b, " VERSION ", pq.QuoteIdentifier(v.(string)))
	}

	if d.Get(extCreateCascadeAttr).(bool) {
		fmt.Fprint(b, " CASCADE")
	}

	txn, err := startTransaction(db.client, databaseName)
	if err != nil {
		return err
	}
	defer deferredRollback(txn)

	sql := b.String()
	if _, err := txn.Exec(sql); err != nil {
		return err
	}

	if err = txn.Commit(); err != nil {
		return fmt.Errorf("error creating extension: %w", err)
	}

	d.SetId(generateExtensionID(d, databaseName))

	return resourcePostgreSQLExtensionReadImpl(db, d)
}

func resourcePostgreSQLExtensionExists(db *DBConnection, d *schema.ResourceData) (bool, error) {
	if !db.featureSupported(featureExtension) {
		return false, fmt.Errorf(
			"postgresql_extension resource is not supported for this Postgres version (%s)",
			db.version,
		)
	}

	var extensionName string

	database, extName, err := getDBExtName(d, db.client)
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

	query := "SELECT extname FROM pg_catalog.pg_extension WHERE extname = $1"
	err = txn.QueryRow(query, extName).Scan(&extensionName)
	switch {
	case err == sql.ErrNoRows:
		return false, nil
	case err != nil:
		return false, err
	}

	return true, nil
}

func resourcePostgreSQLExtensionRead(db *DBConnection, d *schema.ResourceData) error {
	if !db.featureSupported(featureExtension) {
		return fmt.Errorf(
			"postgresql_extension resource is not supported for this Postgres version (%s)",
			db.version,
		)
	}

	return resourcePostgreSQLExtensionReadImpl(db, d)
}

func resourcePostgreSQLExtensionReadImpl(db *DBConnection, d *schema.ResourceData) error {
	database, extName, err := getDBExtName(d, db.client)
	if err != nil {
		return err
	}

	txn, err := startTransaction(db.client, database)
	if err != nil {
		return err
	}
	defer deferredRollback(txn)

	var extSchema, extVersion string
	query := `SELECT n.nspname, e.extversion ` +
		`FROM pg_catalog.pg_extension e, pg_catalog.pg_namespace n ` +
		`WHERE n.oid = e.extnamespace AND e.extname = $1`
	err = txn.QueryRow(query, extName).Scan(&extSchema, &extVersion)
	switch {
	case err == sql.ErrNoRows:
		log.Printf("[WARN] PostgreSQL extension (%s) not found for database %s", extName, database)
		d.SetId("")
		return nil
	case err != nil:
		return fmt.Errorf("error reading extension: %w", err)
	}

	d.Set(extNameAttr, extName)
	d.Set(extSchemaAttr, extSchema)
	d.Set(extVersionAttr, extVersion)
	d.Set(extDatabaseAttr, database)
	d.SetId(generateExtensionID(d, database))

	return nil
}

func resourcePostgreSQLExtensionDelete(db *DBConnection, d *schema.ResourceData) error {
	if !db.featureSupported(featureExtension) {
		return fmt.Errorf(
			"postgresql_extension resource is not supported for this Postgres version (%s)",
			db.version,
		)
	}

	extName := d.Get(extNameAttr).(string)
	database := getDatabaseForExtension(d, db.client.databaseName)

	txn, err := startTransaction(db.client, database)
	if err != nil {
		return err
	}
	defer deferredRollback(txn)

	dropMode := "RESTRICT"
	if d.Get(extDropCascadeAttr).(bool) {
		dropMode = "CASCADE"
	}

	sql := fmt.Sprintf("DROP EXTENSION %s %s ", pq.QuoteIdentifier(extName), dropMode)
	if _, err := txn.Exec(sql); err != nil {
		return err
	}

	if err = txn.Commit(); err != nil {
		return fmt.Errorf("error deleting extension: %w", err)
	}

	d.SetId("")

	return nil
}

func resourcePostgreSQLExtensionUpdate(db *DBConnection, d *schema.ResourceData) error {
	if !db.featureSupported(featureExtension) {
		return fmt.Errorf(
			"postgresql_extension resource is not supported for this Postgres version (%s)",
			db.version,
		)
	}

	database := getDatabaseForExtension(d, db.client.databaseName)
	txn, err := startTransaction(db.client, database)
	if err != nil {
		return err
	}
	defer deferredRollback(txn)

	// Can't rename a schema

	if err := setExtSchema(txn, d); err != nil {
		return err
	}

	if err := setExtVersion(txn, d); err != nil {
		return err
	}

	if err = txn.Commit(); err != nil {
		return fmt.Errorf("error updating extension: %w", err)
	}

	return resourcePostgreSQLExtensionReadImpl(db, d)
}

func setExtSchema(txn *sql.Tx, d *schema.ResourceData) error {
	if !d.HasChange(extSchemaAttr) {
		return nil
	}

	extName := d.Get(extNameAttr).(string)
	_, nraw := d.GetChange(extSchemaAttr)
	n := nraw.(string)
	if n == "" {
		return errors.New("error setting extension name to an empty string")
	}

	sql := fmt.Sprintf("ALTER EXTENSION %s SET SCHEMA %s",
		pq.QuoteIdentifier(extName), pq.QuoteIdentifier(n))
	if _, err := txn.Exec(sql); err != nil {
		return fmt.Errorf("error updating extension SCHEMA: %w", err)
	}

	return nil
}

func setExtVersion(txn *sql.Tx, d *schema.ResourceData) error {
	if !d.HasChange(extVersionAttr) {
		return nil
	}

	extName := d.Get(extNameAttr).(string)

	b := bytes.NewBufferString("ALTER EXTENSION ")
	fmt.Fprintf(b, "%s UPDATE", pq.QuoteIdentifier(extName))

	_, nraw := d.GetChange(extVersionAttr)
	n := nraw.(string)
	if n != "" {
		fmt.Fprintf(b, " TO %s", pq.QuoteIdentifier(n))
	}

	sql := b.String()
	if _, err := txn.Exec(sql); err != nil {
		return fmt.Errorf("error updating extension version: %w", err)
	}

	return nil
}

func getDatabaseForExtension(d *schema.ResourceData, databaseName string) string {
	if v, ok := d.GetOk(extDatabaseAttr); ok {
		databaseName = v.(string)
	}

	return databaseName
}

func generateExtensionID(d *schema.ResourceData, databaseName string) string {
	return strings.Join([]string{
		databaseName,
		d.Get(extNameAttr).(string),
	}, ".")
}

func getExtensionNameFromID(ID string) string {
	splitted := strings.Split(ID, ".")
	return splitted[0]
}

// getDBExtName returns database and extension name. If we are importing this resource, they will be parsed
// from the resource ID (it will return an error if parsing failed) otherwise they will be simply
// get from the state.
func getDBExtName(d *schema.ResourceData, client *Client) (string, string, error) {
	database := getDatabaseForExtension(d, client.databaseName)
	extName := d.Get(extNameAttr).(string)

	// When importing, we have to parse the ID to find extension and database names.
	if extName == "" {
		parsed := strings.Split(d.Id(), ".")
		if len(parsed) != 2 {
			return "", "", fmt.Errorf("extension ID %s has not the expected format 'database.extension': %v", d.Id(), parsed)
		}
		database = parsed[0]
		extName = parsed[1]
	}
	return database, extName, nil
}
