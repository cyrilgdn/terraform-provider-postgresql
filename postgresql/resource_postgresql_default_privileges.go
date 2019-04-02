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

func resourcePostgreSQLDefaultPrivileges() *schema.Resource {
	return &schema.Resource{
		Create: resourcePostgreSQLDefaultPrivilegesCreate,
		Update: resourcePostgreSQLDefaultPrivilegesCreate,
		Read:   resourcePostgreSQLDefaultPrivilegesRead,
		Delete: resourcePostgreSQLDefaultPrivilegesDelete,

		Schema: map[string]*schema.Schema{
			"role": {
				Type:        schema.TypeString,
				Required:    true,
				ForceNew:    true,
				Description: "The name of the role to which grant default privileges on",
			},
			"database": {
				Type:        schema.TypeString,
				Required:    true,
				ForceNew:    true,
				Description: "The database to grant default privileges for this role",
			},
			"owner": {
				Type:        schema.TypeString,
				Required:    true,
				ForceNew:    true,
				Description: "The name of an existing role of which the current (connected user) role is a member (you can change default privileges only for objects that will be created by yourself or by roles that you are a member of)",
			},
			"schema": {
				Type:        schema.TypeString,
				Required:    true,
				ForceNew:    true,
				Description: "The database schema to set default privileges for this role",
			},
			"object_type": {
				Type:     schema.TypeString,
				Required: true,
				ForceNew: true,
				ValidateFunc: validation.StringInSlice([]string{
					"table",
					"sequence",
				}, false),
				Description: "The PostgreSQL object type to set the default privileges on (one of: table, sequence)",
			},
			"privileges": &schema.Schema{
				Type:        schema.TypeSet,
				Required:    true,
				Elem:        &schema.Schema{Type: schema.TypeString},
				Set:         schema.HashString,
				MinItems:    1,
				Description: "The list of privileges to apply as default privileges",
			},
		},
	}
}

func resourcePostgreSQLDefaultPrivilegesRead(d *schema.ResourceData, meta interface{}) error {
	client := meta.(*Client)

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

	txn, err := startTransaction(client, d.Get("database").(string))
	if err != nil {
		return err
	}
	defer deferredRollback(txn)

	return readRoleDefaultPrivileges(txn, d)
}

func resourcePostgreSQLDefaultPrivilegesCreate(d *schema.ResourceData, meta interface{}) error {
	if err := validatePrivileges(d.Get("object_type").(string), d.Get("privileges").(*schema.Set).List()); err != nil {
		return err
	}

	database := d.Get("database").(string)

	client := meta.(*Client)

	client.catalogLock.Lock()
	defer client.catalogLock.Unlock()

	txn, err := startTransaction(client, database)
	if err != nil {
		return err
	}
	defer deferredRollback(txn)

	// Revoke all privileges before granting otherwise reducing privileges will not work.
	// We just have to revoke them in the same transaction so role will not lost his privileges between revoke and grant.
	if err = revokeRoleDefaultPrivileges(txn, d); err != nil {
		return err
	}

	if err = grantRoleDefaultPrivileges(txn, d); err != nil {
		return err
	}

	if err := txn.Commit(); err != nil {
		return err
	}

	d.SetId(generateDefaultPrivilegesID(d))

	txn, err = startTransaction(client, d.Get("database").(string))
	if err != nil {
		return err
	}
	defer deferredRollback(txn)

	return readRoleDefaultPrivileges(txn, d)
}

func resourcePostgreSQLDefaultPrivilegesDelete(d *schema.ResourceData, meta interface{}) error {
	client := meta.(*Client)

	client.catalogLock.Lock()
	defer client.catalogLock.Unlock()

	txn, err := startTransaction(client, d.Get("database").(string))
	if err != nil {
		return err
	}
	defer deferredRollback(txn)

	revokeRoleDefaultPrivileges(txn, d)
	if err := txn.Commit(); err != nil {
		return err
	}

	return nil
}

func readRoleDefaultPrivileges(txn *sql.Tx, d *schema.ResourceData) error {
	role := d.Get("role").(string)
	owner := d.Get("owner").(string)
	pgSchema := d.Get("schema").(string)
	objectType := d.Get("object_type").(string)

	// This query aggregates the list of default privileges type (prtype)
	// for the role (grantee), owner (grantor), schema (namespace name)
	// and the specified object type (defaclobjtype).
	query := `SELECT array_agg(prtype) FROM (
		SELECT defaclnamespace, (aclexplode(defaclacl)).* FROM pg_default_acl
		WHERE defaclobjtype = $3
	) AS t (namespace, grantor_oid, grantee_oid, prtype, grantable)

	JOIN pg_namespace ON pg_namespace.oid = namespace
	WHERE pg_get_userbyid(grantee_oid) = $1 AND nspname = $2 AND pg_get_userbyid(grantor_oid) = $4;
`
	var privileges pq.ByteaArray

	if err := txn.QueryRow(
		query, role, pgSchema, objectTypes[objectType], owner,
	).Scan(&privileges); err != nil {
		return errwrap.Wrapf("could not read default privileges: {{err}}", err)
	}

	// We consider no privileges as "not exists"
	if len(privileges) == 0 {
		log.Printf("[DEBUG] no default privileges for role %s in schema %s", role, pgSchema)
		d.SetId("")
		return nil
	}

	privilegesSet := pgArrayToSet(privileges)
	d.Set("privileges", privilegesSet)
	d.SetId(generateDefaultPrivilegesID(d))

	return nil
}

func grantRoleDefaultPrivileges(txn *sql.Tx, d *schema.ResourceData) error {
	role := d.Get("role").(string)
	pgSchema := d.Get("schema").(string)

	privileges := []string{}
	for _, priv := range d.Get("privileges").(*schema.Set).List() {
		privileges = append(privileges, priv.(string))
	}

	// TODO: We grant default privileges for the DB owner
	// For that we need to be either superuser or a member of the owner role.
	// With AWS RDS, It's not possible to create superusers as it is restricted by AWS itself.
	// In that case, the only solution would be to have the PostgreSQL user used by Terraform
	// to be also part of the database owner role.

	query := fmt.Sprintf("ALTER DEFAULT PRIVILEGES FOR ROLE %s IN SCHEMA %s GRANT %s ON %sS TO %s",
		pq.QuoteIdentifier(d.Get("owner").(string)),
		pq.QuoteIdentifier(pgSchema),
		strings.Join(privileges, ","),
		strings.ToUpper(d.Get("object_type").(string)),
		pq.QuoteIdentifier(role),
	)

	_, err := txn.Exec(
		query,
	)
	if err != nil {
		return errwrap.Wrapf("could not alter default privileges: {{err}}", err)
	}

	return nil
}

func revokeRoleDefaultPrivileges(txn *sql.Tx, d *schema.ResourceData) error {
	query := fmt.Sprintf(
		"ALTER DEFAULT PRIVILEGES FOR ROLE %s IN SCHEMA %s REVOKE ALL ON %sS FROM %s",
		pq.QuoteIdentifier(d.Get("owner").(string)),
		pq.QuoteIdentifier(d.Get("schema").(string)),
		strings.ToUpper(d.Get("object_type").(string)),
		pq.QuoteIdentifier(d.Get("role").(string)),
	)

	_, err := txn.Exec(query)
	return err
}

func generateDefaultPrivilegesID(d *schema.ResourceData) string {
	return strings.Join([]string{
		d.Get("role").(string), d.Get("database").(string), d.Get("schema").(string),
		d.Get("owner").(string), d.Get("object_type").(string),
	}, "_")
}
