package postgresql

import (
	"database/sql"
	"fmt"
	"log"
	"strings"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/validation"

	// Use Postgres as SQL driver
	"github.com/lib/pq"
)

var allowedObjectTypes = []string{
	"database",
	"function",
	"procedure",
	"routine",
	"schema",
	"sequence",
	"table",
	"foreign_data_wrapper",
	"foreign_server",
	"column",
}

var objectTypes = map[string]string{
	"table":    "r",
	"sequence": "S",
	"function": "f",
	"type":     "T",
	"schema":   "n",
}

type ResourceSchemeGetter func(string) interface{}

func resourcePostgreSQLGrant() *schema.Resource {
	return &schema.Resource{
		Create: PGResourceFunc(resourcePostgreSQLGrantCreate),
		Update: PGResourceFunc(resourcePostgreSQLGrantUpdate),
		Read:   PGResourceFunc(resourcePostgreSQLGrantRead),
		Delete: PGResourceFunc(resourcePostgreSQLGrantDelete),

		Schema: map[string]*schema.Schema{
			"role": {
				Type:        schema.TypeString,
				Required:    true,
				ForceNew:    true,
				Description: "The name of the role to grant privileges on",
			},
			"database": {
				Type:        schema.TypeString,
				Required:    true,
				ForceNew:    true,
				Description: "The database to grant privileges on for this role",
			},
			"schema": {
				Type:        schema.TypeString,
				Optional:    true,
				ForceNew:    true,
				Description: "The database schema to grant privileges on for this role",
			},
			"object_type": {
				Type:         schema.TypeString,
				Required:     true,
				ForceNew:     true,
				ValidateFunc: validation.StringInSlice(allowedObjectTypes, false),
				Description:  "The PostgreSQL object type to grant the privileges on (one of: " + strings.Join(allowedObjectTypes, ", ") + ")",
			},
			"objects": {
				Type:        schema.TypeSet,
				Optional:    true,
				ForceNew:    true,
				Elem:        &schema.Schema{Type: schema.TypeString},
				Set:         schema.HashString,
				Description: "The specific objects to grant privileges on for this role (empty means all objects of the requested type)",
			},
			"columns": {
				Type:        schema.TypeSet,
				Optional:    true,
				ForceNew:    true,
				Elem:        &schema.Schema{Type: schema.TypeString},
				Set:         schema.HashString,
				Description: "The specific columns to grant privileges on for this role",
			},
			"privileges": {
				Type:        schema.TypeSet,
				Required:    true,
				Elem:        &schema.Schema{Type: schema.TypeString},
				Set:         schema.HashString,
				Description: "The list of privileges to grant",
			},
			"with_grant_option": {
				Type:        schema.TypeBool,
				Optional:    true,
				ForceNew:    true,
				Default:     false,
				Description: "Permit the grant recipient to grant it to others",
			},
		},
	}
}

func resourcePostgreSQLGrantRead(db *DBConnection, d *schema.ResourceData) error {
	if err := validateFeatureSupport(db, d); err != nil {
		return fmt.Errorf("feature is not supported: %v", err)
	}

	exists, err := checkRoleDBSchemaExists(db, d)
	if err != nil {
		return err
	}
	if !exists {
		d.SetId("")
		return nil
	}
	d.SetId(generateGrantID(d))

	txn, err := startTransaction(db.client, d.Get("database").(string))
	if err != nil {
		return err
	}
	defer deferredRollback(txn)

	return readRolePrivileges(txn, d)
}

func resourcePostgreSQLGrantCreate(db *DBConnection, d *schema.ResourceData) error {
	return resourcePostgreSQLGrantCreateOrUpdate(db, d, false)
}

func resourcePostgreSQLGrantUpdate(db *DBConnection, d *schema.ResourceData) error {
	return resourcePostgreSQLGrantCreateOrUpdate(db, d, true)
}

func resourcePostgreSQLGrantCreateOrUpdate(db *DBConnection, d *schema.ResourceData, usePrevious bool) error {
	if err := validateFeatureSupport(db, d); err != nil {
		return fmt.Errorf("feature is not supported: %v", err)
	}

	// Validate parameters.
	objectType := d.Get("object_type").(string)
	if d.Get("schema").(string) == "" && !sliceContainsStr([]string{"database", "foreign_data_wrapper", "foreign_server"}, objectType) {
		return fmt.Errorf("parameter 'schema' is mandatory for postgresql_grant resource")
	}
	if d.Get("objects").(*schema.Set).Len() > 0 && (objectType == "database" || objectType == "schema") {
		return fmt.Errorf("cannot specify `objects` when `object_type` is `database` or `schema`")
	}
	if d.Get("columns").(*schema.Set).Len() > 0 && (objectType != "column") {
		return fmt.Errorf("cannot specify `columns` when `object_type` is not `column`")
	}
	if d.Get("columns").(*schema.Set).Len() == 0 && (objectType == "column") {
		return fmt.Errorf("must specify `columns` when `object_type` is `column`")
	}
	if d.Get("privileges").(*schema.Set).Len() != 1 && (objectType == "column") {
		return fmt.Errorf("must specify exactly 1 `privileges` when `object_type` is `column`")
	}
	if (d.Get("objects").(*schema.Set).Len() != 1) && (objectType == "column") {
		return fmt.Errorf("must specify exactly 1 table in the `objects` field when `object_type` is `column`")
	}
	if d.Get("objects").(*schema.Set).Len() != 1 && (objectType == "foreign_data_wrapper" || objectType == "foreign_server") {
		return fmt.Errorf("one element must be specified in `objects` when `object_type` is `foreign_data_wrapper` or `foreign_server`")
	}
	if err := validatePrivileges(d); err != nil {
		return err
	}

	database := d.Get("database").(string)

	txn, err := startTransaction(db.client, database)
	if err != nil {
		return err
	}
	defer deferredRollback(txn)

	role := d.Get("role").(string)
	if err := pgLockRole(txn, role); err != nil {
		return err
	}

	if objectType == "database" {
		if err := pgLockDatabase(txn, database); err != nil {
			return err
		}
	}

	owners, err := getRolesToGrant(txn, d)
	if err != nil {
		return err
	}
	if err := withRolesGranted(txn, owners, func() error {
		// Revoke all privileges before granting otherwise reducing privileges will not work.
		// We just have to revoke them in the same transaction so the role will not lose its
		// privileges between the revoke and grant statements.
		if err := revokeRolePrivileges(txn, d, usePrevious); err != nil {
			return err
		}
		if err := grantRolePrivileges(txn, d); err != nil {
			return err
		}
		return nil
	}); err != nil {
		return err
	}

	if err = txn.Commit(); err != nil {
		return fmt.Errorf("could not commit transaction: %w", err)
	}

	d.SetId(generateGrantID(d))

	txn, err = startTransaction(db.client, database)
	if err != nil {
		return err
	}
	defer deferredRollback(txn)

	return readRolePrivileges(txn, d)
}

func resourcePostgreSQLGrantDelete(db *DBConnection, d *schema.ResourceData) error {
	if err := validateFeatureSupport(db, d); err != nil {
		return fmt.Errorf("feature is not supported: %v", err)
	}

	database := d.Get("database").(string)
	txn, err := startTransaction(db.client, database)
	if err != nil {
		return err
	}
	defer deferredRollback(txn)

	role := d.Get("role").(string)
	if err := pgLockRole(txn, role); err != nil {
		return err
	}

	objectType := d.Get("object_type").(string)
	if objectType == "database" {
		if err := pgLockDatabase(txn, database); err != nil {
			return err
		}
	}

	owners, err := getRolesToGrant(txn, d)
	if err != nil {
		return err
	}

	if err := withRolesGranted(txn, owners, func() error {
		return revokeRolePrivileges(txn, d, false)
	}); err != nil {
		return err
	}

	if err = txn.Commit(); err != nil {
		return fmt.Errorf("could not commit transaction: %w", err)
	}

	return nil
}

func readDatabaseRolePrivileges(txn *sql.Tx, d *schema.ResourceData, roleOID uint32) error {
	dbName := d.Get("database").(string)
	query := `
SELECT array_agg(privilege_type)
FROM (
	SELECT (aclexplode(datacl)).* FROM pg_database WHERE datname=$1
) as privileges
WHERE grantee = $2
`

	var privileges pq.ByteaArray
	if err := txn.QueryRow(query, dbName, roleOID).Scan(&privileges); err != nil {
		return fmt.Errorf("could not read privileges for database %s: %w", dbName, err)
	}
	granted := pgArrayToSet(privileges)
	if !resourcePrivilegesEqual(granted, d) {
		return d.Set("privileges", granted)
	}
	return nil
}

func readSchemaRolePrivileges(txn *sql.Tx, d *schema.ResourceData, roleOID uint32) error {
	dbName := d.Get("schema").(string)
	query := `
SELECT array_agg(privilege_type)
FROM (
	SELECT (aclexplode(nspacl)).* FROM pg_namespace WHERE nspname=$1
) as privileges
WHERE grantee = $2
`

	var privileges pq.ByteaArray
	if err := txn.QueryRow(query, dbName, roleOID).Scan(&privileges); err != nil {
		return fmt.Errorf("could not read privileges for schema %s: %w", dbName, err)
	}

	granted := pgArrayToSet(privileges)
	if !resourcePrivilegesEqual(granted, d) {
		return d.Set("privileges", granted)
	}
	return nil
}

func readForeignDataWrapperRolePrivileges(txn *sql.Tx, d *schema.ResourceData, roleOID uint32) error {
	objects := d.Get("objects").(*schema.Set).List()
	fdwName := objects[0].(string)
	query := `
SELECT pg_catalog.array_agg(privilege_type)
FROM (
	SELECT (pg_catalog.aclexplode(fdwacl)).* FROM pg_catalog.pg_foreign_data_wrapper WHERE fdwname=$1
) as privileges
WHERE grantee = $2
`

	var privileges pq.ByteaArray
	if err := txn.QueryRow(query, fdwName, roleOID).Scan(&privileges); err != nil {
		return fmt.Errorf("could not read privileges for foreign data wrapper %s: %w", fdwName, err)
	}

	granted := pgArrayToSet(privileges)
	if !resourcePrivilegesEqual(granted, d) {
		return d.Set("privileges", granted)
	}
	return nil
}

func readForeignServerRolePrivileges(txn *sql.Tx, d *schema.ResourceData, roleOID uint32) error {
	objects := d.Get("objects").(*schema.Set).List()
	srvName := objects[0].(string)
	query := `
SELECT pg_catalog.array_agg(privilege_type)
FROM (
	SELECT (pg_catalog.aclexplode(srvacl)).* FROM pg_catalog.pg_foreign_server WHERE srvname=$1
) as privileges
WHERE grantee = $2
`

	var privileges pq.ByteaArray
	if err := txn.QueryRow(query, srvName, roleOID).Scan(&privileges); err != nil {
		return fmt.Errorf("could not read privileges for foreign server %s: %w", srvName, err)
	}

	granted := pgArrayToSet(privileges)
	if !resourcePrivilegesEqual(granted, d) {
		return d.Set("privileges", granted)
	}
	return nil
}

func readColumnRolePrivileges(txn *sql.Tx, d *schema.ResourceData) error {
	objects := d.Get("objects").(*schema.Set)

	missingColumns := d.Get("columns").(*schema.Set) // Getting columns from state.
	// If the query returns a column, it is a removed from the missingColumns.

	var rows *sql.Rows

	// The attacl column of pg_attribute contains information only about explicit column grants
	query := `
SELECT relname AS table_name, attname AS column_name, array_agg(privilege_type) AS column_privileges
FROM (SELECT relname, attname, (aclexplode(attacl)).*
      FROM pg_class
               JOIN pg_namespace ON pg_class.relnamespace = pg_namespace.oid
               JOIN pg_attribute ON pg_class.oid = attrelid
      WHERE nspname = $2
        AND relname = $3
        AND relkind = $4)
         AS col_privs
         JOIN pg_roles ON pg_roles.oid = col_privs.grantee
WHERE rolname = $1
  AND privilege_type = $5
GROUP BY col_privs.relname, col_privs.attname, col_privs.privilege_type
ORDER BY col_privs.attname
;`
	rows, err := txn.Query(
		query, d.Get("role").(string), d.Get("schema"), objects.List()[0], objectTypes["table"], d.Get("privileges").(*schema.Set).List()[0],
	)

	if err != nil {
		return err
	}

	for rows.Next() {
		var objName string
		var colName string
		var privileges pq.ByteaArray

		if err := rows.Scan(&objName, &colName, &privileges); err != nil {
			return err
		}

		if objects.Len() > 0 && !objects.Contains(objName) {
			continue
		}

		if missingColumns.Contains(colName) {
			missingColumns.Remove(colName)
		}

		privilegesSet := pgArrayToSet(privileges)

		if !privilegesSet.Equal(d.Get("privileges").(*schema.Set)) {
			// If any object doesn't have the same privileges as saved in the state,
			// we return its privileges to force an update.
			log.Printf(
				"[DEBUG] %s %s has not the expected privileges %v for role %s",
				strings.ToTitle("column"), objName, privileges, d.Get("role"),
			)
			d.Set("privileges", privilegesSet)
			break
		}
	}

	if missingColumns.Len() > 0 {
		// If missingColumns is not empty by the end of the result processing loop
		// it means that a column is missing
		remainingColumns := d.Get("columns").(*schema.Set).Difference(missingColumns)
		log.Printf(
			"[DEBUG] Role %s does not have the expected privileges on columns",
			d.Get("role"),
		)
		d.Set("columns", remainingColumns)
	}

	return nil
}

func readRolePrivileges(txn *sql.Tx, d *schema.ResourceData) error {
	role := d.Get("role").(string)
	objectType := d.Get("object_type").(string)
	objects := d.Get("objects").(*schema.Set)

	roleOID, err := getRoleOID(txn, role)
	if err != nil {
		return err
	}

	var query string
	var rows *sql.Rows

	switch objectType {
	case "database":
		return readDatabaseRolePrivileges(txn, d, roleOID)

	case "schema":
		return readSchemaRolePrivileges(txn, d, roleOID)

	case "foreign_data_wrapper":
		return readForeignDataWrapperRolePrivileges(txn, d, roleOID)

	case "foreign_server":
		return readForeignServerRolePrivileges(txn, d, roleOID)

	case "function", "procedure", "routine":
		query = `
SELECT pg_proc.proname, array_remove(array_agg(privilege_type), NULL)
FROM pg_proc
JOIN pg_namespace ON pg_namespace.oid = pg_proc.pronamespace
LEFT JOIN (
    select acls.*
    from (
             SELECT proname, pronamespace, (aclexplode(proacl)).* FROM pg_proc
         ) acls
    WHERE grantee = $1
) privs
USING (proname, pronamespace)
      WHERE nspname = $2
GROUP BY pg_proc.proname
`
		rows, err = txn.Query(
			query, roleOID, d.Get("schema"),
		)

	case "column":
		return readColumnRolePrivileges(txn, d)

	default:
		query = `
SELECT pg_class.relname, array_remove(array_agg(privilege_type), NULL)
FROM pg_class
JOIN pg_namespace ON pg_namespace.oid = pg_class.relnamespace
LEFT JOIN (
    SELECT acls.* FROM (
        SELECT relname, relnamespace, relkind, (aclexplode(relacl)).* FROM pg_class c
    ) as acls
    WHERE grantee=$1
) privs
USING (relname, relnamespace, relkind)
WHERE nspname = $2 AND relkind = $3
GROUP BY pg_class.relname
`
		rows, err = txn.Query(
			query, roleOID, d.Get("schema"), objectTypes[objectType],
		)
	}

	// This returns, for the specified role (rolname),
	// the list of all object of the specified type (relkind) in the specified schema (namespace)
	// with the list of the currently applied privileges (aggregation of privilege_type)
	//
	// Our goal is to check that every object has the same privileges as saved in the state.
	if err != nil {
		return err
	}

	for rows.Next() {
		var objName string
		var privileges pq.ByteaArray

		if err := rows.Scan(&objName, &privileges); err != nil {
			return err
		}

		if objects.Len() > 0 && !objects.Contains(objName) {
			continue
		}

		privilegesSet := pgArrayToSet(privileges)
		if !resourcePrivilegesEqual(privilegesSet, d) {
			// If any object doesn't have the same privileges as saved in the state,
			// we return its privileges to force an update.
			log.Printf(
				"[DEBUG] %s %s has not the expected privileges %v for role %s",
				strings.ToTitle(objectType), objName, privileges, d.Get("role"),
			)
			d.Set("privileges", privilegesSet)
			break
		}
	}

	return nil
}

func createGrantQuery(d *schema.ResourceData, privileges []string) string {
	var query string

	switch strings.ToUpper(d.Get("object_type").(string)) {
	case "DATABASE":
		query = fmt.Sprintf(
			"GRANT %s ON DATABASE %s TO %s",
			strings.Join(privileges, ","),
			pq.QuoteIdentifier(d.Get("database").(string)),
			pq.QuoteIdentifier(d.Get("role").(string)),
		)
	case "SCHEMA":
		query = fmt.Sprintf(
			"GRANT %s ON SCHEMA %s TO %s",
			strings.Join(privileges, ","),
			pq.QuoteIdentifier(d.Get("schema").(string)),
			pq.QuoteIdentifier(d.Get("role").(string)),
		)
	case "FOREIGN_DATA_WRAPPER":
		fdwName := d.Get("objects").(*schema.Set).List()[0]
		query = fmt.Sprintf(
			"GRANT %s ON FOREIGN DATA WRAPPER %s TO %s",
			strings.Join(privileges, ","),
			pq.QuoteIdentifier(fdwName.(string)),
			pq.QuoteIdentifier(d.Get("role").(string)),
		)
	case "FOREIGN_SERVER":
		srvName := d.Get("objects").(*schema.Set).List()[0]
		query = fmt.Sprintf(
			"GRANT %s ON FOREIGN SERVER %s TO %s",
			strings.Join(privileges, ","),
			pq.QuoteIdentifier(srvName.(string)),
			pq.QuoteIdentifier(d.Get("role").(string)),
		)
	case "COLUMN":
		objects := d.Get("objects").(*schema.Set)
		query = fmt.Sprintf(
			"GRANT %s (%s) ON TABLE %s TO %s",
			strings.Join(privileges, ","),
			setToPgIdentListWithoutSchema(d.Get("columns").(*schema.Set)),
			setToPgIdentList(d.Get("schema").(string), objects),
			pq.QuoteIdentifier(d.Get("role").(string)),
		)
	case "TABLE", "SEQUENCE", "FUNCTION", "PROCEDURE", "ROUTINE":
		objects := d.Get("objects").(*schema.Set)
		if objects.Len() > 0 {
			query = fmt.Sprintf(
				"GRANT %s ON %s %s TO %s",
				strings.Join(privileges, ","),
				strings.ToUpper(d.Get("object_type").(string)),
				setToPgIdentList(d.Get("schema").(string), objects),
				pq.QuoteIdentifier(d.Get("role").(string)),
			)
		} else {
			query = fmt.Sprintf(
				"GRANT %s ON ALL %sS IN SCHEMA %s TO %s",
				strings.Join(privileges, ","),
				strings.ToUpper(d.Get("object_type").(string)),
				pq.QuoteIdentifier(d.Get("schema").(string)),
				pq.QuoteIdentifier(d.Get("role").(string)),
			)
		}
	}

	if d.Get("with_grant_option").(bool) {
		query = query + " WITH GRANT OPTION"
	}

	return query
}

func createRevokeQuery(getter ResourceSchemeGetter) string {
	var query string

	switch strings.ToUpper(getter("object_type").(string)) {
	case "DATABASE":
		query = fmt.Sprintf(
			"REVOKE ALL PRIVILEGES ON DATABASE %s FROM %s",
			pq.QuoteIdentifier(getter("database").(string)),
			pq.QuoteIdentifier(getter("role").(string)),
		)
	case "SCHEMA":
		query = fmt.Sprintf(
			"REVOKE ALL PRIVILEGES ON SCHEMA %s FROM %s",
			pq.QuoteIdentifier(getter("schema").(string)),
			pq.QuoteIdentifier(getter("role").(string)),
		)
	case "FOREIGN_DATA_WRAPPER":
		fdwName := getter("objects").(*schema.Set).List()[0]
		query = fmt.Sprintf(
			"REVOKE ALL PRIVILEGES ON FOREIGN DATA WRAPPER %s FROM %s",
			pq.QuoteIdentifier(fdwName.(string)),
			pq.QuoteIdentifier(getter("role").(string)),
		)
	case "FOREIGN_SERVER":
		srvName := getter("objects").(*schema.Set).List()[0]
		query = fmt.Sprintf(
			"REVOKE ALL PRIVILEGES ON FOREIGN SERVER %s FROM %s",
			pq.QuoteIdentifier(srvName.(string)),
			pq.QuoteIdentifier(getter("role").(string)),
		)
	case "COLUMN":
		objects := getter("objects").(*schema.Set)
		columns := getter("columns").(*schema.Set)
		privileges := getter("privileges").(*schema.Set)
		if privileges.Len() == 0 || columns.Len() == 0 {
			// No privileges to revoke, so don't revoke anything
			query = "SELECT NULL"
		} else {
			query = fmt.Sprintf(
				"REVOKE %s (%s) ON TABLE %s FROM %s",
				setToPgIdentSimpleList(privileges),
				setToPgIdentListWithoutSchema(columns),
				setToPgIdentList(getter("schema").(string), objects),
				pq.QuoteIdentifier(getter("role").(string)),
			)
		}
	case "TABLE", "SEQUENCE", "FUNCTION", "PROCEDURE", "ROUTINE":
		objects := getter("objects").(*schema.Set)
		privileges := getter("privileges").(*schema.Set)
		if objects.Len() > 0 {
			if privileges.Len() > 0 {
				// Revoking specific privileges instead of all privileges
				// to avoid messing with column level grants
				query = fmt.Sprintf(
					"REVOKE %s ON %s %s FROM %s",
					setToPgIdentSimpleList(privileges),
					strings.ToUpper(getter("object_type").(string)),
					setToPgIdentList(getter("schema").(string), objects),
					pq.QuoteIdentifier(getter("role").(string)),
				)
			} else {
				query = fmt.Sprintf(
					"REVOKE ALL PRIVILEGES ON %s %s FROM %s",
					strings.ToUpper(getter("object_type").(string)),
					setToPgIdentList(getter("schema").(string), objects),
					pq.QuoteIdentifier(getter("role").(string)),
				)
			}
		} else {
			query = fmt.Sprintf(
				"REVOKE ALL PRIVILEGES ON ALL %sS IN SCHEMA %s FROM %s",
				strings.ToUpper(getter("object_type").(string)),
				pq.QuoteIdentifier(getter("schema").(string)),
				pq.QuoteIdentifier(getter("role").(string)),
			)
		}
	}

	return query
}

func grantRolePrivileges(txn *sql.Tx, d *schema.ResourceData) error {
	privileges := []string{}
	for _, priv := range d.Get("privileges").(*schema.Set).List() {
		privileges = append(privileges, priv.(string))
	}

	if len(privileges) == 0 {
		log.Printf("[DEBUG] no privileges to grant for role %s in database: %s,", d.Get("role").(string), d.Get("database"))
		return nil
	}

	query := createGrantQuery(d, privileges)

	_, err := txn.Exec(query)
	return err
}

func revokeRolePrivileges(txn *sql.Tx, d *schema.ResourceData, usePrevious bool) error {
	getter := d.Get

	if usePrevious {
		getter = func(name string) interface{} {
			if d.HasChange(name) {
				old, _ := d.GetChange(name)
				return old
			}

			return d.Get(name)
		}
	}

	query := createRevokeQuery(getter)
	if len(query) == 0 {
		// Query is empty, don't run anything
		return nil
	}

	// Obtain a lock to avoid `Error: could not execute revoke query: pq: tuple concurrently updated`
	schema := d.Get("schema").(string)
	if err := pgLockSchema(txn, schema); err != nil {
		return err
	}

	if _, err := txn.Exec(query); err != nil {
		return fmt.Errorf("could not execute revoke query: %w", err)
	}
	return nil
}

func checkRoleDBSchemaExists(db *DBConnection, d *schema.ResourceData) (bool, error) {
	// Check the database exists
	database := d.Get("database").(string)
	exists, err := dbExists(db, database)
	if err != nil {
		return false, err
	}
	if !exists {
		log.Printf("[DEBUG] database %s does not exists", database)
		return false, nil
	}

	txn, err := startTransaction(db.client, database)
	if err != nil {
		return false, err
	}
	defer deferredRollback(txn)

	// Check the role exists
	role := d.Get("role").(string)
	if role != publicRole {
		exists, err := roleExists(txn, role)
		if err != nil {
			return false, err
		}
		if !exists {
			log.Printf("[DEBUG] role %s does not exists", role)
			return false, nil
		}
	}

	// Check the schema exists (the SQL connection needs to be on the right database)
	pgSchema := d.Get("schema").(string)
	if !sliceContainsStr([]string{"database", "foreign_data_wrapper", "foreign_server"}, d.Get("object_type").(string)) && pgSchema != "" {
		exists, err = schemaExists(txn, pgSchema)
		if err != nil {
			return false, err
		}
		if !exists {
			log.Printf("[DEBUG] schema %s does not exists", pgSchema)
			return false, nil
		}
	}

	return true, nil
}

func generateGrantID(d *schema.ResourceData) string {
	parts := []string{d.Get("role").(string), d.Get("database").(string)}

	objectType := d.Get("object_type").(string)
	if objectType != "database" && objectType != "foreign_data_wrapper" && objectType != "foreign_server" {
		parts = append(parts, d.Get("schema").(string))
	}
	parts = append(parts, objectType)

	for _, object := range d.Get("objects").(*schema.Set).List() {
		parts = append(parts, object.(string))
	}

	for _, column := range d.Get("columns").(*schema.Set).List() {
		parts = append(parts, column.(string))
	}

	return strings.Join(parts, "_")
}

func getRolesToGrant(txn *sql.Tx, d *schema.ResourceData) ([]string, error) {
	// If user we use for Terraform is not a superuser (e.g.: in RDS)
	// we need to grant owner of the schema and owners of tables in the schema
	// in order to change theirs permissions.
	owners := []string{}
	objectType := d.Get("object_type")

	if objectType == "database" || objectType == "foreign_data_wrapper" || objectType == "foreign_server" {
		return owners, nil
	}

	schemaName := d.Get("schema").(string)

	if objectType != "schema" {
		var err error
		owners, err = getTablesOwner(txn, schemaName)
		if err != nil {
			return nil, err
		}
	}

	schemaOwner, err := getSchemaOwner(txn, schemaName)
	if err != nil {
		return nil, err
	}
	if !sliceContainsStr(owners, schemaOwner) {
		owners = append(owners, schemaOwner)
	}

	owners, err = resolveOwners(txn, owners)
	if err != nil {
		return nil, err
	}

	return owners, nil
}

func validateFeatureSupport(db *DBConnection, d *schema.ResourceData) error {
	if !db.featureSupported(featurePrivileges) {
		return fmt.Errorf(
			"postgresql_grant resource is not supported for this Postgres version (%s)",
			db.version,
		)
	}
	if d.Get("object_type") == "procedure" && !db.featureSupported(featureProcedure) {
		return fmt.Errorf(
			"object type PROCEDURE is not supported for this Postgres version (%s)",
			db.version,
		)
	}
	if d.Get("object_type") == "routine" && !db.featureSupported(featureRoutine) {
		return fmt.Errorf(
			"object type ROUTINE is not supported for this Postgres version (%s)",
			db.version,
		)
	}
	return nil
}
