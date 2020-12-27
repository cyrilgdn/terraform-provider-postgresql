package postgresql

import (
	"database/sql"
	"fmt"
	"log"
	"strconv"
	"strings"

	"github.com/hashicorp/terraform-plugin-sdk/helper/schema"
	"github.com/lib/pq"
)

const (
	// This returns the role membership for role, grant_role
	getGrantRoleQuery = `
SELECT
  pg_get_userbyid(member) as role,
  pg_get_userbyid(roleid) as grant_role,
  admin_option
FROM
  pg_auth_members
WHERE
  pg_get_userbyid(member) = $1 AND
  pg_get_userbyid(roleid) = $2;
`
)

func resourcePostgreSQLGrantRole() *schema.Resource {
	return &schema.Resource{
		Create: resourcePostgreSQLGrantRoleCreate,
		Read:   resourcePostgreSQLGrantRoleRead,
		Delete: resourcePostgreSQLGrantRoleDelete,

		Schema: map[string]*schema.Schema{
			"role": {
				Type:        schema.TypeString,
				Required:    true,
				ForceNew:    true,
				Description: "The name of the role to grant grant_role",
			},
			"grant_role": {
				Type:        schema.TypeString,
				Required:    true,
				ForceNew:    true,
				Description: "The name of the role that is granted to role",
			},
			"with_admin_option": {
				Type:        schema.TypeBool,
				Optional:    true,
				ForceNew:    true,
				Default:     false,
				Description: "Permit the grant recipient to grant it to others",
			},
		},
	}
}

func resourcePostgreSQLGrantRoleRead(d *schema.ResourceData, meta interface{}) error {
	client := meta.(*Client)

	if !client.featureSupported(featurePrivileges) {
		return fmt.Errorf(
			"postgresql_grant_role resource is not supported for this Postgres version (%s)",
			client.version,
		)
	}

	client.catalogLock.RLock()
	defer client.catalogLock.RUnlock()

	return readGrantRole(client.DB(), d)
}

func resourcePostgreSQLGrantRoleCreate(d *schema.ResourceData, meta interface{}) error {
	client := meta.(*Client)

	if !client.featureSupported(featurePrivileges) {
		return fmt.Errorf(
			"postgresql_grant_role resource is not supported for this Postgres version (%s)",
			client.version,
		)
	}

	client.catalogLock.Lock()
	defer client.catalogLock.Unlock()

	txn, err := startTransaction(client, "")
	if err != nil {
		return err
	}
	defer deferredRollback(txn)

	// Revoke the granted roles before granting them again.
	if err = revokeRole(txn, d); err != nil {
		return err
	}

	if err = grantRole(txn, d); err != nil {
		return err
	}

	if err = txn.Commit(); err != nil {
		return fmt.Errorf("could not commit transaction: %w", err)
	}

	d.SetId(generateGrantRoleID(d))

	return readGrantRole(client.DB(), d)
}

func resourcePostgreSQLGrantRoleDelete(d *schema.ResourceData, meta interface{}) error {
	client := meta.(*Client)

	if !client.featureSupported(featurePrivileges) {
		return fmt.Errorf(
			"postgresql_grant_role resource is not supported for this Postgres version (%s)",
			client.version,
		)
	}

	client.catalogLock.Lock()
	defer client.catalogLock.Unlock()

	txn, err := startTransaction(client, "")
	if err != nil {
		return err
	}
	defer deferredRollback(txn)

	if err = revokeRole(txn, d); err != nil {
		return err
	}

	if err = txn.Commit(); err != nil {
		return fmt.Errorf("could not commit transaction: %w", err)
	}

	return nil
}

func readGrantRole(db QueryAble, d *schema.ResourceData) error {
	var roleName, grantRoleName string
	var withAdminOption bool

	grantRoleID := d.Id()

	values := []interface{}{
		&roleName,
		&grantRoleName,
		&withAdminOption,
	}

	err := db.QueryRow(getGrantRoleQuery, d.Get("role"), d.Get("grant_role")).Scan(values...)
	switch {
	case err == sql.ErrNoRows:
		log.Printf("[WARN] PostgreSQL grant role (%q) not found", grantRoleID)
		d.SetId("")
		return nil
	case err != nil:
		return fmt.Errorf("Error reading grant role: %w", err)
	}

	d.Set("role", roleName)
	d.Set("grant_role", grantRoleName)
	d.Set("with_admin_option", withAdminOption)

	d.SetId(generateGrantRoleID(d))

	return nil
}

func createGrantRoleQuery(d *schema.ResourceData) string {
	grantRole, _ := d.Get("grant_role").(string)
	role, _ := d.Get("role").(string)

	query := fmt.Sprintf(
		"GRANT %s TO %s",
		pq.QuoteIdentifier(grantRole),
		pq.QuoteIdentifier(role),
	)
	if wao, _ := d.Get("with_admin_option").(bool); wao {
		query = query + " WITH ADMIN OPTION"
	}

	return query
}

func createRevokeRoleQuery(d *schema.ResourceData) string {
	grantRole, _ := d.Get("grant_role").(string)
	role, _ := d.Get("role").(string)

	return fmt.Sprintf(
		"REVOKE %s FROM %s",
		pq.QuoteIdentifier(grantRole),
		pq.QuoteIdentifier(role),
	)
}

func grantRole(txn *sql.Tx, d *schema.ResourceData) error {
	query := createGrantRoleQuery(d)
	if _, err := txn.Exec(query); err != nil {
		return fmt.Errorf("could not execute grant query: %w", err)
	}
	return nil
}

func revokeRole(txn *sql.Tx, d *schema.ResourceData) error {
	query := createRevokeRoleQuery(d)
	if _, err := txn.Exec(query); err != nil {
		return fmt.Errorf("could not execute revoke query: %w", err)
	}
	return nil
}

func generateGrantRoleID(d *schema.ResourceData) string {
	return strings.Join([]string{d.Get("role").(string), d.Get("grant_role").(string), strconv.FormatBool(d.Get("with_admin_option").(bool))}, "_")
}
