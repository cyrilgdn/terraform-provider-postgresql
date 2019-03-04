package postgresql

import (
	"database/sql"
	"fmt"
	"log"
	"strings"

	"github.com/hashicorp/errwrap"
	"github.com/hashicorp/terraform/helper/schema"
	"github.com/hashicorp/terraform/helper/validation"

	// Use Postgres as SQL driver
	"github.com/lib/pq"
)

var objectTypes = map[string]string{
	"table":    "r",
	"sequence": "S",
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
				Required:    true,
				ForceNew:    true,
				Description: "The database schema to grant privileges on for this role",
			},
			"object_type": {
				Type:     schema.TypeString,
				Required: true,
				ForceNew: true,
				ValidateFunc: validation.StringInSlice([]string{
					"table",
					"sequence",
				}, false),
				Description: "The PostgreSQL object type to grant the privileges on (one of: table, sequence)",
			},
			"privileges": &schema.Schema{
				Type:        schema.TypeSet,
				Required:    true,
				Elem:        &schema.Schema{Type: schema.TypeString},
				Set:         schema.HashString,
				MinItems:    1,
				Description: "The list of privileges to grant",
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

	if err := validatePrivileges(d.Get("object_type").(string), d.Get("privileges").(*schema.Set).List()); err != nil {
		return err
	}

	database := d.Get("database").(string)

	client.catalogLock.Lock()
	defer client.catalogLock.Unlock()

	txn, err := startTransaction(client, database)
	if err != nil {
		return err
	}
	defer deferredRollback(txn)

	// Revoke all privileges before granting otherwise reducing privileges will not work.
	// We just have to revoke them in the same transaction so the role will not lost its
	// privileges between the revoke and grant statements.
	if err = revokeRolePrivileges(txn, d); err != nil {
		return err
	}

	if err = grantRolePrivileges(txn, d); err != nil {
		return err
	}

	if err = txn.Commit(); err != nil {
		return errwrap.Wrapf("could not commit transaction: {{err}}", err)
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

	if err = revokeRolePrivileges(txn, d); err != nil {
		return err
	}

	if err = txn.Commit(); err != nil {
		return errwrap.Wrapf("could not commit transaction: {{err}}", err)
	}

	return nil
}

func readRolePrivileges(txn *sql.Tx, d *schema.ResourceData) error {
	// This returns, for the specified role (rolname),
	// the list of all object of the specified type (relkind) in the specified schema (namespace)
	// with the list of the currently applied privileges (aggregation of privilege_type)
	//
	// Our goal is to check that every object has the same privileges as saved in the state.
	query := `
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
GROUP BY pg_class.relname;
`

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

func grantRolePrivileges(txn *sql.Tx, d *schema.ResourceData) error {
	privileges := []string{}
	for _, priv := range d.Get("privileges").(*schema.Set).List() {
		privileges = append(privileges, priv.(string))
	}

	query := fmt.Sprintf(
		"GRANT %s ON ALL %sS IN SCHEMA %s TO %s",
		strings.Join(privileges, ","),
		strings.ToUpper(d.Get("object_type").(string)),
		pq.QuoteIdentifier(d.Get("schema").(string)),
		pq.QuoteIdentifier(d.Get("role").(string)),
	)

	_, err := txn.Exec(query)
	return err
}

func revokeRolePrivileges(txn *sql.Tx, d *schema.ResourceData) error {
	query := fmt.Sprintf(
		"REVOKE ALL PRIVILEGES ON ALL %sS IN SCHEMA %s FROM %s",
		strings.ToUpper(d.Get("object_type").(string)),
		pq.QuoteIdentifier(d.Get("schema").(string)),
		pq.QuoteIdentifier(d.Get("role").(string)),
	)

	_, err := txn.Exec(query)
	return err
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

	// Connect on this database to check if schema exists
	dbTxn, err := startTransaction(client, database)
	if err != nil {
		return false, err
	}
	defer dbTxn.Rollback()

	// Check the schema exists (the SQL connection needs to be on the right database)
	pgSchema := d.Get("schema").(string)
	exists, err = schemaExists(txn, pgSchema)
	if err != nil {
		return false, err
	}
	if !exists {
		log.Printf("[DEBUG] schema %s does not exists", pgSchema)
		return false, nil
	}

	return true, nil
}

func generateGrantID(d *schema.ResourceData) string {
	return strings.Join([]string{
		d.Get("role").(string), d.Get("database").(string),
		d.Get("schema").(string), d.Get("object_type").(string),
	}, "_")
}
