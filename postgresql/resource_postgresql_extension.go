package postgresql

import (
	"bytes"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"strings"

	"github.com/hashicorp/terraform-plugin-sdk/helper/schema"
	"github.com/lib/pq"
)

const (
	extNameAttr        = "name"
	extSchemaAttr      = "schema"
	extVersionAttr     = "version"
	extDatabaseAttr    = "database"
	extDropCascadeAttr = "drop_cascade"
)

func resourcePostgreSQLExtension() *schema.Resource {
	return &schema.Resource{
		Create: resourcePostgreSQLExtensionCreate,
		Read:   resourcePostgreSQLExtensionRead,
		Update: resourcePostgreSQLExtensionUpdate,
		Delete: resourcePostgreSQLExtensionDelete,
		Exists: resourcePostgreSQLExtensionExists,
		Importer: &schema.ResourceImporter{
			State: schema.ImportStatePassthrough,
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
		},
	}
}

func resourcePostgreSQLExtensionCreate(d *schema.ResourceData, meta interface{}) error {
	c := meta.(*Client)

	if !c.featureSupported(featureExtension) {
		return fmt.Errorf(
			"postgresql_extension resource is not supported for this Postgres version (%s)",
			c.version,
		)
	}

	c.catalogLock.Lock()
	defer c.catalogLock.Unlock()

	extName := d.Get(extNameAttr).(string)

	b := bytes.NewBufferString("CREATE EXTENSION IF NOT EXISTS ")
	fmt.Fprint(b, pq.QuoteIdentifier(extName))

	if v, ok := d.GetOk(extSchemaAttr); ok {
		fmt.Fprint(b, " SCHEMA ", pq.QuoteIdentifier(v.(string)))
	}

	if v, ok := d.GetOk(extVersionAttr); ok {
		fmt.Fprint(b, " VERSION ", pq.QuoteIdentifier(v.(string)))
	}

	database := getDatabaseForExtension(d, c)

	txn, err := startTransaction(c, database)
	if err != nil {
		return err
	}
	defer deferredRollback(txn)

	sql := b.String()
	if _, err := txn.Exec(sql); err != nil {
		return err
	}

	if err = txn.Commit(); err != nil {
		return fmt.Errorf("Error creating extension: %w", err)
	}

	d.SetId(generateExtensionID(d, c))

	return resourcePostgreSQLExtensionReadImpl(d, meta)
}

func resourcePostgreSQLExtensionExists(d *schema.ResourceData, meta interface{}) (bool, error) {
	c := meta.(*Client)

	if !c.featureSupported(featureExtension) {
		return false, fmt.Errorf(
			"postgresql_extension resource is not supported for this Postgres version (%s)",
			c.version,
		)
	}

	c.catalogLock.Lock()
	defer c.catalogLock.Unlock()

	var extensionName string

	database, extName, err := getDBExtName(d, c)
	if err != nil {
		return false, err
	}

	// Check if the database exists
	exists, err := dbExists(c.DB(), database)
	if err != nil || !exists {
		return false, err
	}

	txn, err := startTransaction(c, database)
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

func resourcePostgreSQLExtensionRead(d *schema.ResourceData, meta interface{}) error {
	c := meta.(*Client)

	if !c.featureSupported(featureExtension) {
		return fmt.Errorf(
			"postgresql_extension resource is not supported for this Postgres version (%s)",
			c.version,
		)
	}

	c.catalogLock.RLock()
	defer c.catalogLock.RUnlock()

	return resourcePostgreSQLExtensionReadImpl(d, meta)
}

func resourcePostgreSQLExtensionReadImpl(d *schema.ResourceData, meta interface{}) error {
	c := meta.(*Client)
	database, extName, err := getDBExtName(d, c)
	if err != nil {
		return err
	}

	txn, err := startTransaction(c, database)
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
		return fmt.Errorf("Error reading extension: %w", err)
	}

	d.Set(extNameAttr, extName)
	d.Set(extSchemaAttr, extSchema)
	d.Set(extVersionAttr, extVersion)
	d.Set(extDatabaseAttr, database)
	d.SetId(generateExtensionID(d, c))

	return nil
}

func resourcePostgreSQLExtensionDelete(d *schema.ResourceData, meta interface{}) error {
	c := meta.(*Client)

	if !c.featureSupported(featureExtension) {
		return fmt.Errorf(
			"postgresql_extension resource is not supported for this Postgres version (%s)",
			c.version,
		)
	}

	c.catalogLock.Lock()
	defer c.catalogLock.Unlock()

	extName := d.Get(extNameAttr).(string)

	database := getDatabaseForExtension(d, c)
	txn, err := startTransaction(c, database)
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
		return fmt.Errorf("Error deleting extension: %w", err)
	}

	d.SetId("")

	return nil
}

func resourcePostgreSQLExtensionUpdate(d *schema.ResourceData, meta interface{}) error {
	c := meta.(*Client)

	if !c.featureSupported(featureExtension) {
		return fmt.Errorf(
			"postgresql_extension resource is not supported for this Postgres version (%s)",
			c.version,
		)
	}

	c.catalogLock.Lock()
	defer c.catalogLock.Unlock()

	database := getDatabaseForExtension(d, c)
	txn, err := startTransaction(c, database)
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
		return fmt.Errorf("Error updating extension: %w", err)
	}

	return resourcePostgreSQLExtensionReadImpl(d, meta)
}

func setExtSchema(txn *sql.Tx, d *schema.ResourceData) error {
	if !d.HasChange(extSchemaAttr) {
		return nil
	}

	extName := d.Get(extNameAttr).(string)
	_, nraw := d.GetChange(extSchemaAttr)
	n := nraw.(string)
	if n == "" {
		return errors.New("Error setting extension name to an empty string")
	}

	sql := fmt.Sprintf("ALTER EXTENSION %s SET SCHEMA %s",
		pq.QuoteIdentifier(extName), pq.QuoteIdentifier(n))
	if _, err := txn.Exec(sql); err != nil {
		return fmt.Errorf("Error updating extension SCHEMA: %w", err)
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
		return fmt.Errorf("Error updating extension version: %w", err)
	}

	return nil
}

func getDatabaseForExtension(d *schema.ResourceData, client *Client) string {
	database := client.databaseName
	if v, ok := d.GetOk(extDatabaseAttr); ok {
		database = v.(string)
	}

	return database
}

func generateExtensionID(d *schema.ResourceData, client *Client) string {
	return strings.Join([]string{
		getDatabaseForExtension(d, client), d.Get(extNameAttr).(string),
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
	database := getDatabaseForExtension(d, client)
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
