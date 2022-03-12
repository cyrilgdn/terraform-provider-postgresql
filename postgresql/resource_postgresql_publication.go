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
	pubNameAttr                    = "name"
	pubOwnerAttr                   = "owner"
	pubDatabaseAttr                = "database"
	pubAllTablesAttr               = "all_tables"
	pubTablesAttr                  = "tables"
	pubDropCascadeAttr             = "drop_cascade"
	pubPublishAttr                 = "publish_param"
	pubPublisViaPartitionRoothAttr = "publis_via_partition_rooth_param"
)

func resourcePostgreSQLPublication() *schema.Resource {
	return &schema.Resource{
		Create: PGResourceFunc(resourcePostgreSQLPublicationCreate),
		Read:   PGResourceFunc(resourcePostgreSQLPublicationRead),
		Delete: PGResourceFunc(resourcePostgreSQLPublicationDelete),
		Update: PGResourceFunc(resourcePostgreSQLPublicationUpdate),
		Exists: PGResourceExistsFunc(resourcePostgreSQLPublicationExists),
		Importer: &schema.ResourceImporter{
			StateContext: schema.ImportStatePassthroughContext,
		},

		Schema: map[string]*schema.Schema{
			pubNameAttr: {
				Type:     schema.TypeString,
				Required: true,
				ForceNew: true,
			},
			pubDatabaseAttr: {
				Type:        schema.TypeString,
				Required:    true,
				ForceNew:    true,
				Description: "Sets the database to add the publication for",
			},
			pubOwnerAttr: {
				Type:        schema.TypeString,
				Optional:    true,
				Computed:    true,
				ForceNew:    false,
				Description: "Sets the owner of the publication",
			},
			pubTablesAttr: {
				Type:     schema.TypeSet,
				Optional: true,
				ForceNew: false,
				Elem: &schema.Schema{
					Type: schema.TypeString,
				},
				Set:           schema.HashString,
				Description:   "Sets the tables list to publish",
				ConflictsWith: []string{pubAllTablesAttr},
			},
			pubAllTablesAttr: {
				Type:          schema.TypeBool,
				Optional:      true,
				ForceNew:      true,
				Description:   "Sets the tables list to publish to ALL tables",
				ConflictsWith: []string{pubTablesAttr},
			},
			pubPublishAttr: {
				Type:     schema.TypeSet,
				Optional: true,
				Computed: true,
				Elem: &schema.Schema{
					Type: schema.TypeString,
				},
				MinItems:    1,
				Set:         schema.HashString,
				Description: "Sets which DML operations will be published",
			},
			pubPublisViaPartitionRoothAttr: {
				Type:        schema.TypeBool,
				Optional:    true,
				ForceNew:    false,
				Description: "Sets whether changes in a partitioned table using the identity and schema of the partitioned table",
			},
			pubDropCascadeAttr: {
				Type:        schema.TypeBool,
				Optional:    true,
				Default:     false,
				Description: "When true, will also drop all the objects that depend on the extension, and in turn all objects that depend on those objects",
			},
		},
	}
}

func resourcePostgreSQLPublicationUpdate(db *DBConnection, d *schema.ResourceData) error {
	database := getDatabaseForExtension(d, db.client.databaseName)
	txn, err := startTransaction(db.client, database)
	if err != nil {
		return err
	}

	defer deferredRollback(txn)

	if err := setPubOwner(txn, d); err != nil {
		return err
	}

	if err := setPubTables(txn, d); err != nil {
		return err
	}

	if err := setPubParams(txn, d); err != nil {
		return err
	}

	if err := setPubName(txn, d); err != nil {
		return err
	}

	if err = txn.Commit(); err != nil {
		return fmt.Errorf("Error updating publication: %w", err)
	}
	return resourcePostgreSQLDatabaseReadImpl(db, d)
}

func setPubName(txn *sql.Tx, d *schema.ResourceData) error {
	if !d.HasChange(pubNameAttr) {
		return nil
	}
	oraw, nraw := d.GetChange(pubNameAttr)
	o := oraw.(string)
	n := nraw.(string)
	if n == "" {
		return errors.New("Error setting publication name to an empty string")
	}

	sql := fmt.Sprintf("ALTER PUBLICATION %s RENAME TO %s", pq.QuoteIdentifier(o), pq.QuoteIdentifier(n))
	if _, err := txn.Exec(sql); err != nil {
		return fmt.Errorf("Error updating publication name: %w", err)
	}
	return nil
}

func setPubOwner(txn *sql.Tx, d *schema.ResourceData) error {
	if !d.HasChange(pubOwnerAttr) {
		return nil
	}

	_, nraw := d.GetChange(pubOwnerAttr)
	n := nraw.(string)
	if n == "" {
		return errors.New("Error setting publication owner to an empty string")
	}
	pubName := d.Get(pubNameAttr).(string)

	sql := fmt.Sprintf("ALTER PUBLICATION %s OWNER TO %s", pubName, n)
	if _, err := txn.Exec(sql); err != nil {
		return fmt.Errorf("Error updating publication owner: %w", err)
	}
	return nil
}

func setPubTables(txn *sql.Tx, d *schema.ResourceData) error {
	if !d.HasChange(pubTablesAttr) {
		return nil
	}

	var queries []string
	pubName := d.Get(pubNameAttr).(string)

	oraw, nraw := d.GetChange(pubTablesAttr)
	oldSet := oraw.(*schema.Set)
	newSet := nraw.(*schema.Set)
	dropped := oldSet.Difference(newSet).List()
	added := newSet.Difference(oldSet).List()

	for _, p := range added {
		queryBody := fmt.Sprintf("ALTER PUBLICATION %s ADD TABLE %s", pubName, p)
		queries = append(queries, fmt.Sprintf("%s %s", pubName, queryBody))
	}

	for _, p := range dropped {
		queryBody := fmt.Sprintf("ALTER PUBLICATION %s DROP TABLE %s", pubName, p)
		queries = append(queries, fmt.Sprintf("%s %s", pubName, queryBody))
	}

	for _, query := range queries {
		if _, err := txn.Exec(query); err != nil {
			return err
		}
	}
	return nil
}

func setPubParams(txn *sql.Tx, d *schema.ResourceData) error {
	pubName := d.Get(pubNameAttr).(string)
	param_alter_template := "ALTER PUBLICATION %s SET (%s = %s)"
	if d.HasChange(pubPublishAttr) {
		param_name := "publish"
		_, nraw := d.GetChange(pubPublishAttr)
		var newSet []string

		for _, elem := range nraw.(*schema.Set).List() {
			newSet = append(newSet, elem.(string))
		}

		sql := fmt.Sprintf(param_alter_template, pubName, param_name, pqQuoteLiteral(strings.Join(newSet, ", ")))
		if _, err := txn.Exec(sql); err != nil {
			return fmt.Errorf("Error updating publication paramter '%s': %w", param_name, err)
		}

	}

	if d.HasChange(pubPublisViaPartitionRoothAttr) {
		param_name := "publish_via_partition_root"
		_, nraw := d.GetChange(pubPublisViaPartitionRoothAttr)

		sql := fmt.Sprintf(param_alter_template, pubName, param_name, nraw.(bool))
		if _, err := txn.Exec(sql); err != nil {
			return fmt.Errorf("Error updating publication paramter '%s': %w", param_name, err)
		}
	}

	return nil
}

func resourcePostgreSQLPublicationCreate(db *DBConnection, d *schema.ResourceData) error {

	name := d.Get(pubNameAttr).(string)
	databaseName := getDatabaseForPublication(d, db.client.databaseName)
	tables, err := getTablesForPublication(d)
	if err != nil {
		return err
	}
	publicationParameters, err := getPublicationParameters(d)
	if err != nil {
		return err
	}
	txn, err := startTransaction(db.client, databaseName)
	if err != nil {
		return err
	}
	defer deferredRollback(txn)

	b := bytes.NewBufferString("CREATE PUBLICATION ")
	fmt.Fprint(b, name, " ", tables, " ", publicationParameters)

	sql := b.String()
	if _, err := txn.Exec(sql); err != nil {
		return err
	}
	if err := setPubOwner(txn, d); err != nil {
		return err
	}

	if err = txn.Commit(); err != nil {
		return fmt.Errorf("Error creating Publication: %w", err)
	}

	d.SetId(generatePublicationID(d, databaseName))

	return resourcePostgreSQLPublicationReadImpl(db, d)
}

func resourcePostgreSQLPublicationExists(db *DBConnection, d *schema.ResourceData) (bool, error) {

	var PublicationName string

	database, PublicationName, err := getDBPublicationName(d, db.client)
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

	query := "SELECT pubname FROM pg_catalog.pg_publication WHERE pubname = $1"
	err = txn.QueryRow(query, pqQuoteLiteral(PublicationName)).Scan(&PublicationName)
	switch {
	case err == sql.ErrNoRows:
		return false, nil
	case err != nil:
		return false, err
	}

	return true, nil
}

func resourcePostgreSQLPublicationRead(db *DBConnection, d *schema.ResourceData) error {
	return resourcePostgreSQLPublicationReadImpl(db, d)
}

func resourcePostgreSQLPublicationReadImpl(db *DBConnection, d *schema.ResourceData) error {
	database, PublicationName, err := getDBPublicationName(d, db.client)
	if err != nil {
		return err
	}

	txn, err := startTransaction(db.client, database)
	if err != nil {
		return err
	}
	defer deferredRollback(txn)

	var tableFullNames pq.ByteaArray
	var puballtables, pubinsert, pubupdate, pubdelete, pubtruncate, pubviaroot bool
	var pubowner string
	columns := []string{"puballtables", "pubinsert", "pubupdate", "pubdelete", "pubtruncate", "r.rolname as pubownername"}
	values := []interface{}{
		&puballtables,
		&pubinsert,
		&pubupdate,
		&pubdelete,
		&pubtruncate,
		&pubowner,
	}

	if db.version.Major > 13 {
		columns = append(columns, "pubviaroot")
		values = append(values, &pubviaroot)
	}

	query := fmt.Sprintf("SELECT %s FROM pg_catalog.pg_publication as p join pg_catalog.pg_roles as r on p.pubowner = r.oid WHERE pubname = $1", strings.Join(columns, ", "))
	err = txn.QueryRow(query, PublicationName).Scan(values...)
	switch {
	case err == sql.ErrNoRows:
		log.Printf("[WARN] PostgreSQL Publication (%s) not found for database %s", PublicationName, database)
		d.SetId("")
		return nil
	case err != nil:
		return fmt.Errorf("Error reading publication: %w", err)
	}

	query = `SELECT schemaname, tablename ` +
		`FROM pg_catalog.pg_publication_tables ` +
		`WHERE pubname = $1`

	err = txn.QueryRow(query, pqQuoteLiteral(PublicationName)).Scan(&tableFullNames)
	switch {
	case err == sql.ErrNoRows:
		log.Printf("[WARN] No PostgreSQL tables found for Publication %s", PublicationName)
	case err != nil:
		return fmt.Errorf("Error reading Publication: %w", err)
	}

	tables := pgArrayToSet(tableFullNames)
	publishParams := schema.NewSet(schema.HashString, make([]interface{}, 0))
	if pubinsert {
		publishParams.Add("insert")
	}
	if pubupdate {
		publishParams.Add("update")
	}
	if pubdelete {
		publishParams.Add("delete")
	}
	if pubtruncate {
		publishParams.Add("truncate")
	}
	d.SetId(generatePublicationID(d, database))
	d.Set(pubNameAttr, PublicationName)
	d.Set(pubDatabaseAttr, database)
	d.Set(pubOwnerAttr, pubowner)
	d.Set(pubTablesAttr, tables)
	d.Set(pubPublishAttr, publishParams)
	if sliceContainsStr(columns, "pubviaroot") {
		d.Set(pubPublisViaPartitionRoothAttr, pubviaroot)
	}
	return nil
}

func resourcePostgreSQLPublicationDelete(db *DBConnection, d *schema.ResourceData) error {

	PublicationName := d.Get(pubNameAttr).(string)
	database := getDatabaseForPublication(d, db.client.databaseName)

	txn, err := startTransaction(db.client, database)
	if err != nil {
		return err
	}
	defer deferredRollback(txn)
	dropMode := "RESTRICT"
	if d.Get(extDropCascadeAttr).(bool) {
		dropMode = "CASCADE"
	}

	sql := fmt.Sprintf("DROP PUBLICATION %s %s", pq.QuoteIdentifier(PublicationName), dropMode)
	if _, err := txn.Exec(sql); err != nil {
		return err
	}

	if err = txn.Commit(); err != nil {
		return fmt.Errorf("Error deleting Publication: %w", err)
	}
	d.SetId("")

	return nil
}

func getDatabaseForPublication(d *schema.ResourceData, databaseName string) string {
	if v, ok := d.GetOk(pubDatabaseAttr); ok {
		databaseName = v.(string)
	}

	return databaseName
}

func getTablesForPublication(d *schema.ResourceData) (string, error) {
	var tablesString string
	tables, ok := d.GetOk(pubAllTablesAttr)
	isAllTables, isAllOk := d.GetOk(pubAllTablesAttr)

	if isAllOk {
		if ok {
			return tablesString, fmt.Errorf("Attribute %s cannot be used when %s is true", pubAllTablesAttr, pubAllTablesAttr)
		}
		if isAllTables.(bool) {
			tablesString = "FOR ALL TABLES"
		}
	} else {
		if ok {
			tablesString = fmt.Sprintf("FOR TABLE %s", strings.Join(tables.([]string), ", "))
		}
	}

	return tablesString, nil
}

func getPublicationParameters(d *schema.ResourceData) (string, error) {
	parametersString := ""
	var attrs []string
	if v, ok := d.GetOk(pubPublishAttr); ok {
		validation := []string{"insert", "update", "delete", "truncate"}
		for _, attr := range v.([]string) {
			if !sliceContainsStr(validation, attr) {
				return parametersString, fmt.Errorf("Invalid value of %s: %s. Should be at least on of '%s'", pubPublishAttr, attr, strings.Join(validation, ", "))
			}
		}

		attrs = append(attrs, fmt.Sprintf("publish = '%s'", strings.Join(v.([]string), ", ")))
	}
	if v, ok := d.GetOk(pubPublisViaPartitionRoothAttr); ok {
		attrs = append(attrs, fmt.Sprintf("publish_via_partition_root = %v", v.(bool)))
	}
	if len(attrs) > 0 {
		parametersString = fmt.Sprintf("WITH %s", strings.Join(attrs, ", "))
	}
	return parametersString, nil
}

func generatePublicationID(d *schema.ResourceData, databaseName string) string {
	return strings.Join([]string{
		databaseName,
		d.Get(pubNameAttr).(string),
	}, ".")
}

// getDBPublicationName returns database and publication name. If we are importing this
// resource, they will be parsed from the resource ID (it will return an error if parsing failed)
// otherwise they will be simply get from the state.
func getDBPublicationName(d *schema.ResourceData, client *Client) (string, string, error) {
	database := getDatabaseForPublication(d, client.databaseName)
	PublicationName := d.Get(pubNameAttr).(string)

	// When importing, we have to parse the ID to find publication and database names.
	if PublicationName == "" {
		parsed := strings.Split(d.Id(), ".")
		if len(parsed) != 2 {
			return "", "", fmt.Errorf("Publication ID %s has not the expected format 'database.publication_name': %v", d.Id(), parsed)
		}
		database = parsed[0]
		PublicationName = parsed[1]
	}
	return database, PublicationName, nil
}

func getPublicationNameFromID(ID string) string {
	splitted := strings.Split(ID, ".")
	return splitted[0]
}
