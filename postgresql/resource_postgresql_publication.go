package postgresql

import (
	"database/sql"
	"fmt"
	"log"
	"strings"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/validation"
	"github.com/lib/pq"
)

const (
	pubNameAttr                    = "name"
	pubOwnerAttr                   = "owner"
	pubDatabaseAttr                = "database"
	pubAllTablesAttr               = "all_tables"
	pubTablesAttr                  = "tables"
	pubSchemasAttr                 = "schemas"
	pubDropCascadeAttr             = "drop_cascade"
	pubPublishAttr                 = "publish_param"
	pubPublishViaPartitionRootAttr = "publish_via_partition_root_param"
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
				Type:         schema.TypeString,
				Required:     true,
				ForceNew:     false,
				ValidateFunc: validation.StringIsNotEmpty,
			},
			pubDatabaseAttr: {
				Type:        schema.TypeString,
				Optional:    true,
				Computed:    true,
				ForceNew:    true,
				Description: "Sets the database to add the publication for",
			},
			pubOwnerAttr: {
				Type:         schema.TypeString,
				Optional:     true,
				Computed:     true,
				ForceNew:     false,
				Description:  "Sets the owner of the publication",
				ValidateFunc: validation.StringIsNotEmpty,
			},
			pubTablesAttr: {
				Type:          schema.TypeSet,
				Optional:      true,
				Computed:      true,
				ForceNew:      false,
				Elem:          &schema.Schema{Type: schema.TypeString},
				Description:   "Sets the tables list to publish",
				ConflictsWith: []string{pubAllTablesAttr},
			},
			pubSchemasAttr: {
				Type:          schema.TypeSet,
				Optional:      true,
				ForceNew:      false,
				Elem:          &schema.Schema{Type: schema.TypeString},
				Description:   "Sets the schemas whose tables should be published. Requires PostgreSQL 15 or above",
				ConflictsWith: []string{pubAllTablesAttr},
			},
			pubAllTablesAttr: {
				Type:        schema.TypeBool,
				Optional:    true,
				Computed:    true,
				ForceNew:    true,
				Description: "Sets the tables list to publish to ALL tables",
			},
			pubPublishAttr: {
				Type:        schema.TypeList,
				Optional:    true,
				Computed:    true,
				MinItems:    1,
				Elem:        &schema.Schema{Type: schema.TypeString},
				Description: "Sets which DML operations will be published",
			},
			pubPublishViaPartitionRootAttr: {
				Type:        schema.TypeBool,
				Optional:    true,
				ForceNew:    false,
				Description: "Sets whether changes in a partitioned table using the identity and schema of the partitioned table",
			},
			pubDropCascadeAttr: {
				Type:        schema.TypeBool,
				Optional:    true,
				Default:     false,
				Description: "When true, will also drop all the objects that depend on the publication, and in turn all objects that depend on those objects",
			},
		},
	}
}

func resourcePostgreSQLPublicationUpdate(db *DBConnection, d *schema.ResourceData) error {
	if !db.featureSupported(featurePublication) {
		return fmt.Errorf(
			"postgresql_publication resource is not supported for this Postgres version (%s)",
			db.version,
		)
	}

	database := getDatabaseForPublication(d, db.client.databaseName)
	txn, err := startTransaction(db.client, database)
	if err != nil {
		return fmt.Errorf("could not start transaction: %w", err)
	}

	defer deferredRollback(txn)

	if err := setPubOwner(txn, d); err != nil {
		return fmt.Errorf("could not update publication owner: %w", err)
	}

	if err := setPubTablesAndSchemas(txn, d, db.featureSupported(featurePublicationSchemas)); err != nil {
		return fmt.Errorf("could not update publication tables or schemas: %w", err)
	}

	if err := setPubParams(txn, d, db.featureSupported(featurePublishViaRoot)); err != nil {
		return fmt.Errorf("could not update publication tables: %w", err)
	}

	if err := setPubName(txn, d); err != nil {
		return fmt.Errorf("could not update publication name: %w", err)
	}

	if err = txn.Commit(); err != nil {
		return fmt.Errorf("error updating publication: %w", err)
	}
	return resourcePostgreSQLPublicationReadImpl(db, d)
}

func setPubName(txn *sql.Tx, d *schema.ResourceData) error {
	if !d.HasChange(pubNameAttr) {
		return nil
	}
	oraw, nraw := d.GetChange(pubNameAttr)
	o := oraw.(string)
	n := nraw.(string)
	database := d.Get(pubDatabaseAttr).(string)
	sql := fmt.Sprintf("ALTER PUBLICATION %s RENAME TO %s", pq.QuoteIdentifier(o), pq.QuoteIdentifier(n))
	if _, err := txn.Exec(sql); err != nil {
		return fmt.Errorf("error updating publication name: %w", err)
	}
	d.SetId(generatePublicationID(d, database))
	return nil
}

func setPubOwner(txn *sql.Tx, d *schema.ResourceData) error {
	if !d.HasChange(pubOwnerAttr) {
		return nil
	}

	_, nraw := d.GetChange(pubOwnerAttr)
	n := nraw.(string)
	pubName := d.Get(pubNameAttr).(string)

	sql := fmt.Sprintf("ALTER PUBLICATION %s OWNER TO %s", pubName, n)
	if _, err := txn.Exec(sql); err != nil {
		return fmt.Errorf("error updating publication owner: %w", err)
	}
	return nil
}

func setPubTablesAndSchemas(txn *sql.Tx, d *schema.ResourceData, pubSchemasEnabled bool) error {
	if !d.HasChange(pubTablesAttr) && !d.HasChange(pubSchemasAttr) {
		return nil
	}
	if d.HasChange(pubSchemasAttr) && !pubSchemasEnabled {
		return fmt.Errorf("schemas attribute is supported only for postgres version 15 and above")
	}

	var queries []string
	pubName := d.Get(pubNameAttr).(string)

	oldTablesRaw, newTablesRaw := d.GetChange(pubTablesAttr)
	oldTables := oldTablesRaw.(*schema.Set).List()
	newTables := newTablesRaw.(*schema.Set).List()
	if elem, ok := isUniqueArr(newTables); !ok {
		return fmt.Errorf("'%s' is duplicated for attribute `%s`", elem.(string), pubTablesAttr)
	}

	oldSchemasRaw, newSchemasRaw := d.GetChange(pubSchemasAttr)
	oldSchemas := oldSchemasRaw.(*schema.Set).List()
	newSchemas := newSchemasRaw.(*schema.Set).List()
	if elem, ok := isUniqueArr(newSchemas); !ok {
		return fmt.Errorf("'%s' is duplicated for attribute `%s`", elem.(string), pubSchemasAttr)
	}

	if err := validatePublicationTablesAndSchemas(newTables, newSchemas); err != nil {
		return err
	}

	droppedSchemas := arrayDifference(oldSchemas, newSchemas)
	addedSchemas := arrayDifference(newSchemas, oldSchemas)
	droppedTables := arrayDifference(oldTables, newTables)
	addedTables := arrayDifference(newTables, oldTables)

	for _, p := range droppedSchemas {
		query := fmt.Sprintf("ALTER PUBLICATION %s DROP TABLES IN SCHEMA %s", pubName, pq.QuoteIdentifier(p.(string)))
		queries = append(queries, query)
	}

	for _, p := range droppedTables {
		query := fmt.Sprintf("ALTER PUBLICATION %s DROP TABLE %s", pubName, quoteTableName(p.(string)))
		queries = append(queries, query)
	}

	for _, p := range addedSchemas {
		query := fmt.Sprintf("ALTER PUBLICATION %s ADD TABLES IN SCHEMA %s", pubName, pq.QuoteIdentifier(p.(string)))
		queries = append(queries, query)
	}

	for _, p := range addedTables {
		query := fmt.Sprintf("ALTER PUBLICATION %s ADD TABLE %s", pubName, quoteTableName(p.(string)))
		queries = append(queries, query)
	}

	for _, query := range queries {
		if _, err := txn.Exec(query); err != nil {
			return fmt.Errorf("could not alter publication tables or schemas: %w", err)
		}
	}
	return nil
}

func setPubParams(txn *sql.Tx, d *schema.ResourceData, pubViaRootEnabled bool) error {
	pubName := d.Get(pubNameAttr).(string)
	paramAlterTemplate := "ALTER PUBLICATION %s %s"
	publicationParametersString, err := getPublicationParameters(d, pubViaRootEnabled)
	if err != nil {
		return fmt.Errorf("error getting publication parameters: %w", err)
	}
	if publicationParametersString != "" {
		sql := fmt.Sprintf(paramAlterTemplate, pubName, publicationParametersString)
		if _, err := txn.Exec(sql); err != nil {
			return fmt.Errorf("error updating publication parameters: %w", err)
		}
	}
	return nil
}

func resourcePostgreSQLPublicationCreate(db *DBConnection, d *schema.ResourceData) error {
	if !db.featureSupported(featurePublication) {
		return fmt.Errorf(
			"postgresql_publication resource is not supported for this Postgres version (%s)",
			db.version,
		)
	}

	name := d.Get(pubNameAttr).(string)
	databaseName := getDatabaseForPublication(d, db.client.databaseName)
	tables, err := getTablesForPublication(d, db.featureSupported(featurePublicationSchemas))
	if err != nil {
		return fmt.Errorf("could not get tables for publication: %w", err)
	}
	publicationParameters, err := getPublicationParameters(d, db.featureSupported(featurePublishViaRoot))
	if err != nil {
		return fmt.Errorf("could not get publication parameters: %w", err)
	}
	txn, err := startTransaction(db.client, databaseName)
	if err != nil {
		return fmt.Errorf("could not start transaction: %w", err)
	}
	defer deferredRollback(txn)

	sql := fmt.Sprintf("CREATE PUBLICATION %s %s %s", name, tables, publicationParameters)

	if _, err := txn.Exec(sql); err != nil {
		return fmt.Errorf("error creating Publication: %w", err)
	}
	if err := setPubOwner(txn, d); err != nil {
		return fmt.Errorf("could not set publication owner during creation: %w", err)
	}

	if err = txn.Commit(); err != nil {
		return fmt.Errorf("error creating Publication: %w", err)
	}

	d.SetId(generatePublicationID(d, databaseName))

	return resourcePostgreSQLPublicationReadImpl(db, d)
}

func resourcePostgreSQLPublicationExists(db *DBConnection, d *schema.ResourceData) (bool, error) {
	if !db.featureSupported(featurePublication) {
		return false, fmt.Errorf(
			"postgresql_publication resource is not supported for this Postgres version (%s)",
			db.version,
		)
	}

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
	if !db.featureSupported(featurePublication) {
		return fmt.Errorf(
			"postgresql_publication resource is not supported for this Postgres version (%s)",
			db.version,
		)
	}

	database, PublicationName, err := getDBPublicationName(d, db.client)
	if err != nil {
		return fmt.Errorf("could not get publication name: %w", err)
	}

	txn, err := startTransaction(db.client, database)
	if err != nil {
		return fmt.Errorf("could not start transaction: %w", err)
	}
	defer deferredRollback(txn)

	var tables []string
	var schemas []string
	var publishParams []string
	var puballtables, pubinsert, pubupdate, pubdelete, pubtruncate, pubviaroot bool
	var pubowner string
	columns := []string{"puballtables", "pubinsert", "pubupdate", "pubdelete", "r.rolname as pubownername"}
	values := []any{
		&puballtables,
		&pubinsert,
		&pubupdate,
		&pubdelete,
		&pubowner,
	}

	if db.featureSupported(featurePublishViaRoot) {
		columns = append(columns, "pubviaroot")
		values = append(values, &pubviaroot)
	}
	if db.featureSupported(featurePubTruncate) {
		columns = append(columns, "pubtruncate")
		values = append(values, &pubtruncate)
	}

	query := fmt.Sprintf("SELECT %s FROM pg_catalog.pg_publication as p join pg_catalog.pg_roles as r on p.pubowner = r.oid WHERE pubname = $1", strings.Join(columns, ", "))
	err = txn.QueryRow(query, pqQuoteLiteral(PublicationName)).Scan(values...)

	switch {
	case err == sql.ErrNoRows:
		log.Printf("[WARN] PostgreSQL Publication (%s) not found for database %s", PublicationName, database)
		d.SetId("")
		return nil
	case err != nil:
		return fmt.Errorf("error reading publication info: %w", err)
	}

	if puballtables {
		query = `SELECT CONCAT(schemaname,'.',tablename) as fulltablename ` +
			`FROM pg_catalog.pg_publication_tables ` +
			`WHERE pubname = $1`
	} else {
		query = `SELECT CONCAT(n.nspname,'.',c.relname) as fulltablename ` +
			`FROM pg_catalog.pg_publication_rel pr ` +
			`JOIN pg_catalog.pg_publication p ON p.oid = pr.prpubid ` +
			`JOIN pg_catalog.pg_class c ON c.oid = pr.prrelid ` +
			`JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace ` +
			`WHERE p.pubname = $1`
	}

	rows, err := txn.Query(query, pqQuoteLiteral(PublicationName))
	if err != nil {
		return fmt.Errorf("could not get publication tables: %w", err)
	}

	for rows.Next() {
		var table string
		err := rows.Scan(&table)
		if err != nil {
			return fmt.Errorf("could not get tables: %w", err)
		}
		tables = append(tables, table)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("got rows.Err: %w", err)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("error closing publication table rows: %w", err)
	}

	if db.featureSupported(featurePublicationSchemas) {
		query = `SELECT n.nspname ` +
			`FROM pg_catalog.pg_publication_namespace pn ` +
			`JOIN pg_catalog.pg_publication p ON p.oid = pn.pnpubid ` +
			`JOIN pg_catalog.pg_namespace n ON n.oid = pn.pnnspid ` +
			`WHERE p.pubname = $1`

		rows, err := txn.Query(query, pqQuoteLiteral(PublicationName))
		if err != nil {
			return fmt.Errorf("could not get publication schemas: %w", err)
		}

		for rows.Next() {
			var schema string
			err := rows.Scan(&schema)
			if err != nil {
				return fmt.Errorf("could not get schemas: %w", err)
			}
			schemas = append(schemas, schema)
		}
		if err := rows.Err(); err != nil {
			return fmt.Errorf("got rows.Err: %w", err)
		}
		if err := rows.Close(); err != nil {
			return fmt.Errorf("error closing publication schema rows: %w", err)
		}
	}

	if pubinsert {
		publishParams = append(publishParams, "insert")
	}
	if pubupdate {
		publishParams = append(publishParams, "update")
	}
	if pubdelete {
		publishParams = append(publishParams, "delete")
	}
	if pubtruncate {
		publishParams = append(publishParams, "truncate")
	}

	d.SetId(generatePublicationID(d, database))
	d.Set(pubNameAttr, PublicationName)
	d.Set(pubDatabaseAttr, database)
	d.Set(pubOwnerAttr, pubowner)
	d.Set(pubTablesAttr, tables)
	d.Set(pubSchemasAttr, schemas)
	d.Set(pubAllTablesAttr, puballtables)
	d.Set(pubPublishAttr, publishParams)
	if sliceContainsStr(columns, "pubviaroot") {
		d.Set(pubPublishViaPartitionRootAttr, pubviaroot)
	}
	return nil
}

func resourcePostgreSQLPublicationDelete(db *DBConnection, d *schema.ResourceData) error {
	if !db.featureSupported(featurePublication) {
		return fmt.Errorf(
			"postgresql_publication resource is not supported for this Postgres version (%s)",
			db.version,
		)
	}

	publicationName := d.Get(pubNameAttr).(string)
	database := getDatabaseForPublication(d, db.client.databaseName)

	txn, err := startTransaction(db.client, database)
	if err != nil {
		return fmt.Errorf("could not start transaction: %w", err)
	}
	defer deferredRollback(txn)
	dropMode := "RESTRICT"
	if d.Get(pubDropCascadeAttr).(bool) {
		dropMode = "CASCADE"
	}

	sql := fmt.Sprintf("DROP PUBLICATION %s %s", pq.QuoteIdentifier(publicationName), dropMode)
	if _, err := txn.Exec(sql); err != nil {
		return fmt.Errorf("could not execute sql: %w", err)
	}

	if err = txn.Commit(); err != nil {
		return fmt.Errorf("error deleting Publication: %w", err)
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

func getTablesForPublication(d *schema.ResourceData, pubSchemasEnabled bool) (string, error) {
	setTables, ok := d.GetOk(pubTablesAttr)
	setSchemas, schemasOk := d.GetOk(pubSchemasAttr)
	isAllTables, isAllOk := d.GetOk(pubAllTablesAttr)

	if isAllOk {
		if isAllTables.(bool) {
			return "FOR ALL TABLES", nil
		}
	}

	var publicationObjects []string
	var tables []any
	if ok {
		tables = setTables.(*schema.Set).List()
		var tlist []string
		if elem, ok := isUniqueArr(tables); !ok {
			return "", fmt.Errorf("'%s' is duplicated for attribute `%s`", elem.(string), pubTablesAttr)
		}
		for _, t := range tables {
			tlist = append(tlist, quoteTableName(t.(string)))
		}
		publicationObjects = append(publicationObjects, fmt.Sprintf("TABLE %s", strings.Join(tlist, ", ")))
	}
	if schemasOk {
		if !pubSchemasEnabled {
			return "", fmt.Errorf("schemas attribute is supported only for postgres version 15 and above")
		}

		schemas := setSchemas.(*schema.Set).List()
		var slist []string
		if elem, ok := isUniqueArr(schemas); !ok {
			return "", fmt.Errorf("'%s' is duplicated for attribute `%s`", elem.(string), pubSchemasAttr)
		}
		for _, s := range schemas {
			slist = append(slist, pq.QuoteIdentifier(s.(string)))
		}
		if err := validatePublicationTablesAndSchemas(tables, schemas); err != nil {
			return "", err
		}
		publicationObjects = append(publicationObjects, fmt.Sprintf("TABLES IN SCHEMA %s", strings.Join(slist, ", ")))
	}

	if len(publicationObjects) == 0 {
		return "", nil
	}

	return fmt.Sprintf("FOR %s", strings.Join(publicationObjects, ", ")), nil
}

func validatePublicationTablesAndSchemas(tables []any, schemas []any) error {
	if len(tables) == 0 || len(schemas) == 0 {
		return nil
	}

	schemaNames := make(map[string]bool, len(schemas))
	for _, schemaName := range schemas {
		schemaNames[schemaName.(string)] = true
	}

	for _, table := range tables {
		tableName := table.(string)
		tableSchema := "public"
		parts := strings.Split(tableName, ".")
		if len(parts) > 1 {
			tableSchema = parts[0]
		}
		if schemaNames[tableSchema] {
			// Keep Terraform ownership unambiguous: schema membership is tracked
			// via `schemas`, while explicit table membership is tracked via
			// `tables`.
			return fmt.Errorf("table %s cannot be explicitly published because schema %s is also published", tableName, tableSchema)
		}
	}

	return nil
}

func validatedPublicationPublishParams(paramList []any) ([]string, error) {
	var attrs []string
	if elem, ok := isUniqueArr(paramList); !ok {
		return make([]string, 0), fmt.Errorf("'%s' is duplicated for attribute `%s`", elem.(string), pubTablesAttr)
	}

	validation := []string{"insert", "update", "delete", "truncate"}
	for _, attr := range paramList {
		if !sliceContainsStr(validation, attr.(string)) {
			return make([]string, 0), fmt.Errorf("invalid value of `%s`: %s. Should be at least one of '%s'", pubPublishAttr, attr, strings.Join(validation, ", "))
		}
		attrs = append(attrs, attr.(string))
	}

	return attrs, nil
}

func getPublicationParameters(d *schema.ResourceData, pubViaRootEnabled bool) (string, error) {
	parameterSQLTemplate := ""
	returnValue := ""
	pubParams := make(map[string]string, 2)
	if d.IsNewResource() {
		if v, ok := d.GetOk(pubPublishViaPartitionRootAttr); ok {
			if !pubViaRootEnabled {
				return "", fmt.Errorf(
					"publish_via_partition_root attribute is supported only for postgres version 13 and above",
				)
			}
			pubParams["publish_via_partition_root"] = fmt.Sprintf("%v", v.(bool))
		}

		if v, ok := d.GetOk(pubPublishAttr); ok {
			if paramsList, err := validatedPublicationPublishParams(v.([]any)); err != nil {
				return "", err
			} else {
				pubParams["publish"] = fmt.Sprintf("'%s'", strings.Join(paramsList, ", "))
			}
		}

		parameterSQLTemplate = "WITH (%s)"

	} else {

		if d.HasChange(pubPublishViaPartitionRootAttr) {
			if !pubViaRootEnabled {
				return "", fmt.Errorf(
					"publish_via_partition_root attribute is supported only for postgres version 13 and above",
				)
			}
			_, nraw := d.GetChange(pubPublishViaPartitionRootAttr)
			pubParams["publish_via_partition_root"] = fmt.Sprintf("%v", nraw.(bool))
		}

		if d.HasChange(pubPublishAttr) {
			_, nraw := d.GetChange(pubPublishAttr)
			if paramsList, err := validatedPublicationPublishParams(nraw.([]any)); err != nil {
				return "", err
			} else {
				pubParams["publish"] = fmt.Sprintf("'%s'", strings.Join(paramsList, ", "))
			}
		}
		parameterSQLTemplate = "SET (%s)"

	}
	var paramsList []string
	for k, v := range pubParams {
		paramsList = append(paramsList, fmt.Sprintf("%s = %s", k, v))
	}
	if len(paramsList) > 0 {
		returnValue = fmt.Sprintf(parameterSQLTemplate, strings.Join(paramsList, ","))
	}
	return returnValue, nil
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
			return "", "", fmt.Errorf("publication ID %s has not the expected format 'database.publication_name': %v", d.Id(), parsed)
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
