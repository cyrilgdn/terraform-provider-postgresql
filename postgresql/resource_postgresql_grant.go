package postgresql

import (
	"database/sql"
	"fmt"
	"log"
	"strings"

	"github.com/hashicorp/terraform-plugin-sdk/helper/schema"
	"github.com/hashicorp/terraform-plugin-sdk/helper/validation"

	// Use Postgres as SQL driver
	"github.com/lib/pq"
)

var allowedObjectTypes = []string{
	"database",
	"table",
	"sequence",
	"function",
}

var objectTypes = map[string]string{
	"table":    "r",
	"sequence": "S",
	"function": "f",
	"type":     "T",
}

func resourcePostgreSQLGrant() *schema.Resource {
	return &schema.Resource{
		Create: resourcePostgreSQLGrantCreate,
		// As create revokes and grants we can use it to update too
		Update: resourcePostgreSQLGrantCreate,
		Read:   resourcePostgreSQLGrantRead,
		Delete: resourcePostgreSQLGrantDelete,

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
			"privileges": &schema.Schema{
				Type:        schema.TypeSet,
				Required:    true,
				Elem:        &schema.Schema{Type: schema.TypeString},
				Set:         schema.HashString,
				MinItems:    1,
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

func resourcePostgreSQLGrantRead(d *schema.ResourceData, meta interface{}) error {
	client := meta.(*Client)

	if !client.featureSupported(featurePrivileges) {
		return fmt.Errorf(
			"postgresql_grant resource is not supported for this Postgres version (%s)",
			client.version,
		)
	}

	client.catalogLock.RLock()
	defer client.catalogLock.RUnlock()

	exists, err := checkRoleDBSchemaExists(client, d)
	if err != nil {
		return err
	}
	if !exists {
		d.SetId("")
		return nil
	}
	d.SetId(generateGrantID(d))

	txn, err := startTransaction(client, d.Get("database").(string))
	if err != nil {
		return err
	}
	defer deferredRollback(txn)

	return readRolePrivileges(txn, d)
}

func resourcePostgreSQLGrantCreate(d *schema.ResourceData, meta interface{}) error {
	client := meta.(*Client)

	if !client.featureSupported(featurePrivileges) {
		return fmt.Errorf(
			"postgresql_grant resource is not supported for this Postgres version (%s)",
			client.version,
		)
	}

	if err := validatePrivileges(d); err != nil {
		return err
	}

	database := d.Get("database").(string)
	schemaName := d.Get("schema").(string)

	client.catalogLock.Lock()
	defer client.catalogLock.Unlock()

	txn, err := startTransaction(client, database)
	if err != nil {
		return err
	}
	defer deferredRollback(txn)

	owners := []string{}
	if d.Get("object_type").(string) != "database" {
		owners, err = getRolesToGrantForSchema(txn, schemaName)
		if err != nil {
			return err
		}
	}

	if err := withRolesGranted(txn, owners, func() error {
		// Revoke all privileges before granting otherwise reducing privileges will not work.
		// We just have to revoke them in the same transaction so the role will not lost its
		// privileges between the revoke and grant statements.
		if err := revokeRolePrivileges(txn, d); err != nil {
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

	txn, err = startTransaction(client, database)
	if err != nil {
		return err
	}
	defer deferredRollback(txn)

	return readRolePrivileges(txn, d)
}

func resourcePostgreSQLGrantDelete(d *schema.ResourceData, meta interface{}) error {
	client := meta.(*Client)

	if !client.featureSupported(featurePrivileges) {
		return fmt.Errorf(
			"postgresql_grant resource is not supported for this Postgres version (%s)",
			client.version,
		)
	}

	client.catalogLock.Lock()
	defer client.catalogLock.Unlock()

	txn, err := startTransaction(client, d.Get("database").(string))
	if err != nil {
		return err
	}
	defer deferredRollback(txn)

	owners := []string{}
	if d.Get("object_type").(string) != "database" {
		owners, err = getRolesToGrantForSchema(txn, d.Get("schema").(string))
		if err != nil {
			return err
		}
	}

	if err := withRolesGranted(txn, owners, func() error {
		return revokeRolePrivileges(txn, d)
	}); err != nil {
		return err
	}

	if err = txn.Commit(); err != nil {
		return fmt.Errorf("could not commit transaction: %w", err)
	}

	return nil
}

func readDatabaseRolePriviges(txn *sql.Tx, d *schema.ResourceData) error {
	query := `
SELECT privilege_type
FROM (
	SELECT (aclexplode(datacl)).* FROM pg_database WHERE datname=$1
) as privileges
JOIN pg_roles ON grantee = pg_roles.oid WHERE rolname = $2
`

	privileges := []string{}
	rows, err := txn.Query(query, d.Get("database"), d.Get("role"))
	if err != nil {
		return fmt.Errorf("could not read database privileges: %w", err)
	}

	for rows.Next() {
		var privilegeType string
		if err := rows.Scan(&privilegeType); err != nil {
			return fmt.Errorf("could not scan database privilege: %w", err)
		}
		privileges = append(privileges, privilegeType)
	}

	d.Set("privileges", privileges)
	return nil
}

func readRolePrivileges(txn *sql.Tx, d *schema.ResourceData) error {
	var query string
	object_type := strings.ToUpper(d.Get("object_type").(string))
	switch object_type {
	case "DATABASE":
		return readDatabaseRolePriviges(txn, d)
	case "FUNCTION":
		query = `
SELECT pg_proc.proname, array_remove(array_agg(privilege_type), NULL)
FROM pg_proc
JOIN pg_namespace ON pg_namespace.oid = pg_proc.pronamespace
LEFT JOIN (
    select acls.*
    from (
             SELECT proname, prokind, pronamespace, (aclexplode(proacl)).* FROM pg_proc
         ) acls
    JOIN pg_roles on grantee = pg_roles.oid
    WHERE rolname = $1
) privs
USING (proname, pronamespace, relkind)
      WHERE nspname = $2 AND relkind = $3
GROUP BY pg_proc.proname
`
	default:
		query = `
SELECT pg_class.relname, array_remove(array_agg(privilege_type), NULL)
FROM pg_class
JOIN pg_namespace ON pg_namespace.oid = pg_class.relnamespace
LEFT JOIN (
    SELECT acls.* FROM (
        SELECT relname, relnamespace, relkind, (aclexplode(relacl)).* FROM pg_class c
    ) as acls
    JOIN pg_roles on grantee = pg_roles.oid
    WHERE rolname=$1
) privs
USING (relname, relnamespace, relkind)
WHERE nspname = $2 AND relkind = $3
GROUP BY pg_class.relname
`
	}

	// This returns, for the specified role (rolname),
	// the list of all object of the specified type (relkind) in the specified schema (namespace)
	// with the list of the currently applied privileges (aggregation of privilege_type)
	//
	// Our goal is to check that every object has the same privileges as saved in the state.

	objectType := d.Get("object_type").(string)
	rows, err := txn.Query(
		query, d.Get("role"), d.Get("schema"), objectTypes[objectType],
	)
	if err != nil {
		return err
	}

	for rows.Next() {
		var objName string
		var privileges pq.ByteaArray

		if err := rows.Scan(&objName, &privileges); err != nil {
			return err
		}
		privilegesSet := pgArrayToSet(privileges)

		if !privilegesSet.Equal(d.Get("privileges").(*schema.Set)) {
			// If any object doesn't have the same privileges as saved in the state,
			// we return an empty privileges to force an update.
			log.Printf(
				"[DEBUG] %s %s has not the expected privileges %v for role %s",
				strings.ToTitle(objectType), objName, privileges, d.Get("role"),
			)
			d.Set("privileges", schema.NewSet(schema.HashString, []interface{}{}))
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
	case "TABLE", "SEQUENCE", "FUNCTION":
		query = fmt.Sprintf(
			"GRANT %s ON ALL %sS IN SCHEMA %s TO %s",
			strings.Join(privileges, ","),
			strings.ToUpper(d.Get("object_type").(string)),
			pq.QuoteIdentifier(d.Get("schema").(string)),
			pq.QuoteIdentifier(d.Get("role").(string)),
		)
	}

	if d.Get("with_grant_option").(bool) == true {
		query = query + " WITH GRANT OPTION"
	}

	return query
}

func createRevokeQuery(d *schema.ResourceData) string {
	var query string

	switch strings.ToUpper(d.Get("object_type").(string)) {
	case "DATABASE":
		query = fmt.Sprintf(
			"REVOKE ALL PRIVILEGES ON DATABASE %s FROM %s",
			pq.QuoteIdentifier(d.Get("database").(string)),
			pq.QuoteIdentifier(d.Get("role").(string)),
		)
	case "TABLE", "SEQUENCE", "FUNCTION":
		query = fmt.Sprintf(
			"REVOKE ALL PRIVILEGES ON ALL %sS IN SCHEMA %s FROM %s",
			strings.ToUpper(d.Get("object_type").(string)),
			pq.QuoteIdentifier(d.Get("schema").(string)),
			pq.QuoteIdentifier(d.Get("role").(string)),
		)
	}

	return query
}

func grantRolePrivileges(txn *sql.Tx, d *schema.ResourceData) error {
	privileges := []string{}
	for _, priv := range d.Get("privileges").(*schema.Set).List() {
		privileges = append(privileges, priv.(string))
	}

	query := createGrantQuery(d, privileges)

	_, err := txn.Exec(query)
	return err
}

func revokeRolePrivileges(txn *sql.Tx, d *schema.ResourceData) error {
	query := createRevokeQuery(d)
	if _, err := txn.Exec(query); err != nil {
		return fmt.Errorf("could not execute revoke query: %w", err)
	}
	return nil
}

func checkRoleDBSchemaExists(client *Client, d *schema.ResourceData) (bool, error) {
	txn, err := startTransaction(client, "")
	if err != nil {
		return false, err
	}
	defer deferredRollback(txn)

	// Check the role exists
	role := d.Get("role").(string)
	exists, err := roleExists(txn, role)
	if err != nil {
		return false, err
	}
	if !exists {
		log.Printf("[DEBUG] role %s does not exists", role)
		return false, nil
	}

	// Check the database exists
	database := d.Get("database").(string)
	exists, err = dbExists(txn, database)
	if err != nil {
		return false, err
	}
	if !exists {
		log.Printf("[DEBUG] database %s does not exists", database)
		return false, nil
	}

	if d.Get("object_type").(string) != "database" {
		// Connect on this database to check if schema exists
		dbTxn, err := startTransaction(client, database)
		if err != nil {
			return false, err
		}
		defer dbTxn.Rollback()

		// Check the schema exists (the SQL connection needs to be on the right database)
		pgSchema := d.Get("schema").(string)
		exists, err = schemaExists(dbTxn, pgSchema)
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
	if objectType != "database" {
		parts = append(parts, d.Get("schema").(string))
	}
	parts = append(parts, objectType)

	return strings.Join(parts, "_")
}

func getRolesToGrantForSchema(txn *sql.Tx, schemaName string) ([]string, error) {
	// If user we use for Terraform is not a superuser (e.g.: in RDS)
	// we need to grant owner of the schema and owners of tables in the schema
	// in order to change theirs permissions.
	owners, err := getTablesOwner(txn, schemaName)
	if err != nil {
		return nil, err
	}

	schemaOwner, err := getSchemaOwner(txn, schemaName)
	if err != nil {
		return nil, err
	}
	if !sliceContainsStr(owners, schemaOwner) {
		owners = append(owners, schemaOwner)
	}

	return owners, nil
}
