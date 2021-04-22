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

var allowedResourceObjectTypes = []string{
	"function",
	"sequence",
	"table",
}

var resourceObjectTypes = map[string]string{
	"table":    "r",
	"sequence": "S",
	"function": "f",
	"type":     "T",
}

func resourcePostgreSQLGrantResource() *schema.Resource {
	return &schema.Resource{
		Create: PGResourceFunc(resourcePostgreSQLGrantResourceCreate),
		Read:   PGResourceFunc(resourcePostgreSQLGrantResourceRead),
		Delete: PGResourceFunc(resourcePostgreSQLGrantResourceDelete),

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
				Type:         schema.TypeString,
				Required:     true,
				ForceNew:     true,
				ValidateFunc: validation.StringInSlice(allowedResourceObjectTypes, false),
				Description:  "The PostgreSQL object type to grant the privileges on (one of: " + strings.Join(allowedResourceObjectTypes, ", ") + ")",
			},
			"privileges": {
				Type:        schema.TypeSet,
				Required:    true,
				Elem:        &schema.Schema{Type: schema.TypeString},
				Set:         schema.HashString,
				ForceNew:    true,
				Description: "The list of privileges to grant",
			},
			"resources": {
				Type:        schema.TypeSet,
				Required:    true,
				Elem:        &schema.Schema{Type: schema.TypeString},
				Set:         schema.HashString,
				ForceNew:    true,
				MinItems:    1,
				Description: "The name of the object, on which the grant should be applied on",
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

func resourcePostgreSQLGrantResourceRead(db *DBConnection, d *schema.ResourceData) error {
	if !db.featureSupported(featurePrivileges) {
		return fmt.Errorf(
			"postgresql_grant_resource resource is not supported for this Postgres version (%s)",
			db.version,
		)
	}

	exists, err := checkRoleDBSchemaExists(db.client, d)
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

	return readRoleResourcePrivileges(txn, d)
}

func resourcePostgreSQLGrantResourceCreate(db *DBConnection, d *schema.ResourceData) error {
	if !db.featureSupported(featurePrivileges) {
		return fmt.Errorf(
			"postgresql_grant_resource resource is not supported for this Postgres version (%s)",
			db.version,
		)
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

	owners, err := getRolesToGrant(txn, d)
	if err != nil {
		return err
	}
	if err := withRolesGranted(txn, owners, func() error {
		// Revoke all privileges before granting otherwise reducing privileges will not work.
		// We just have to revoke them in the same transaction so the role will not lost its
		// privileges between the revoke and grant statements.
		if err := revokeRoleResourcePrivileges(txn, d); err != nil {
			return err
		}
		if err := grantRoleResourcePrivileges(txn, d); err != nil {
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

	return readRoleResourcePrivileges(txn, d)
}

func resourcePostgreSQLGrantResourceDelete(db *DBConnection, d *schema.ResourceData) error {
	if !db.featureSupported(featurePrivileges) {
		return fmt.Errorf(
			"postgresql_grant_resource resource is not supported for this Postgres version (%s)",
			db.version,
		)
	}

	txn, err := startTransaction(db.client, d.Get("database").(string))
	if err != nil {
		return err
	}
	defer deferredRollback(txn)

	owners, err := getRolesToGrant(txn, d)
	if err != nil {
		return err
	}

	if err := withRolesGranted(txn, owners, func() error {
		return revokeRoleResourcePrivileges(txn, d)
	}); err != nil {
		return err
	}

	if err = txn.Commit(); err != nil {
		return fmt.Errorf("could not commit transaction: %w", err)
	}

	return nil
}

func readRoleResourcePrivileges(txn *sql.Tx, d *schema.ResourceData) error {
	role := d.Get("role").(string)
	objectType := d.Get("object_type").(string)

	roleOID, err := getRoleOID(txn, role)
	if err != nil {
		return err
	}

	var query string
	var rows *sql.Rows

	var resourcesList []string
	for _, priv := range d.Get("resources").(*schema.Set).List() {
		resourcesList = append(resourcesList, pq.QuoteLiteral(priv.(string)))
	}

	if objectType == "function" {
		query = fmt.Sprintf(`
SELECT pg_proc.proname, array_remove(array_agg(privilege_type), NULL)
FROM pg_proc
JOIN pg_namespace ON pg_namespace.oid = pg_proc.pronamespace
LEFT JOIN (
    SELECT acls.*
    FROM (
             SELECT proname, pronamespace, (aclexplode(proacl)).* FROM pg_proc
         ) acls
    WHERE grantee = $1
) privs
USING (proname, pronamespace)
      WHERE nspname = $2 AND proname IN (%s)
GROUP BY pg_proc.proname
`, strings.Join(resourcesList, ","))
		rows, err = txn.Query(
			query, roleOID, d.Get("schema"),
		)
	} else {
		query = fmt.Sprintf(`
SELECT pg_class.relname, array_remove(array_agg(privilege_type), NULL)
FROM pg_class
JOIN pg_namespace ON pg_namespace.oid = pg_class.relnamespace
LEFT JOIN (
    SELECT acls.* FROM (
        SELECT relname, relnamespace, relkind, (aclexplode(relacl)).* FROM pg_class C
    ) AS acls
    WHERE grantee=$1
) privs
USING (relname, relnamespace, relkind)
WHERE nspname = $2 AND relkind = $3 AND relname IN (%s)
GROUP BY pg_class.relname
`, strings.Join(resourcesList, ","))
		rows, err = txn.Query(
			query, roleOID, d.Get("schema"), resourceObjectTypes[objectType],
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
		privilegesSet := pgArrayToSet(privileges)

		if !privilegesSet.Equal(d.Get("privileges").(*schema.Set)) {
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

func createGrantResourceQuery(d *schema.ResourceData, privileges []string, resources []string) string {
	var query string

	var schemaName = pq.QuoteIdentifier(d.Get("schema").(string))
	var quotedResources []string
	for _, object := range resources {
		quotedResources = append(quotedResources, schemaName+"."+pq.QuoteIdentifier(object))
	}

	query = fmt.Sprintf(
		"GRANT %s ON %s %s TO %s",
		strings.Join(privileges, ","),
		strings.ToUpper(d.Get("object_type").(string)),
		strings.Join(quotedResources, ","),
		pq.QuoteIdentifier(d.Get("role").(string)),
	)

	if d.Get("with_grant_option").(bool) == true {
		query = query + " WITH GRANT OPTION"
	}

	return query
}

func createRevokeResourceQuery(d *schema.ResourceData, resources []string) string {
	var query string

	var schemaName = pq.QuoteIdentifier(d.Get("schema").(string))
	var quotedResources []string
	for _, object := range resources {
		quotedResources = append(quotedResources, schemaName+"."+pq.QuoteIdentifier(object))
	}

	query = fmt.Sprintf(
		"REVOKE ALL PRIVILEGES ON %s %s FROM %s",
		strings.ToUpper(d.Get("object_type").(string)),
		strings.Join(quotedResources, ","),
		pq.QuoteIdentifier(d.Get("role").(string)),
	)

	return query
}

func grantRoleResourcePrivileges(txn *sql.Tx, d *schema.ResourceData) error {
	var privileges []string
	for _, priv := range d.Get("privileges").(*schema.Set).List() {
		privileges = append(privileges, priv.(string))
	}

	if len(privileges) == 0 {
		log.Printf("[DEBUG] no privileges to grant for role %s in database: %s,", d.Get("role").(string), d.Get("database"))
		return nil
	}

	var resources []string
	for _, priv := range d.Get("resources").(*schema.Set).List() {
		resources = append(resources, priv.(string))
	}

	query := createGrantResourceQuery(d, privileges, resources)

	_, err := txn.Exec(query)
	return err
}

func revokeRoleResourcePrivileges(txn *sql.Tx, d *schema.ResourceData) error {
	var resources []string = nil
	for _, priv := range d.Get("resources").(*schema.Set).List() {
		resources = append(resources, priv.(string))
	}

	query := createRevokeResourceQuery(d, resources)
	if _, err := txn.Exec(query); err != nil {
		return fmt.Errorf("could not execute revoke query: %w", err)
	}
	return nil
}
