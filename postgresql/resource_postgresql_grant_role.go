package postgresql

import (
	"database/sql"
	"fmt"
	"log"
	"strconv"
	"strings"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
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

	getGrantRoleQueryWithMembershipOptions = `
SELECT
  pg_get_userbyid(member) as role,
  pg_get_userbyid(roleid) as grant_role,
  admin_option,
  inherit_option,
  set_option
FROM
  pg_auth_members
WHERE
  pg_get_userbyid(member) = $1 AND
  pg_get_userbyid(roleid) = $2;
`
)

func resourcePostgreSQLGrantRole() *schema.Resource {
	return &schema.Resource{
		Create: PGResourceFunc(resourcePostgreSQLGrantRoleCreate),
		Read:   PGResourceFunc(resourcePostgreSQLGrantRoleRead),
		Delete: PGResourceFunc(resourcePostgreSQLGrantRoleDelete),

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
			"with_inherit_option": {
				Type:        schema.TypeBool,
				Optional:    true,
				Computed:    true,
				ForceNew:    true,
				Description: "Sets the INHERIT option for the grant (PostgreSQL 16+). When omitted, PostgreSQL follows the member role's INHERIT attribute.",
			},
			"with_set_option": {
				Type:        schema.TypeBool,
				Optional:    true,
				Computed:    true,
				ForceNew:    true,
				Description: "Sets the SET option for the grant (PostgreSQL 16+). When omitted, PostgreSQL defaults to SET true.",
			},
		},
	}
}

func resourcePostgreSQLGrantRoleRead(db *DBConnection, d *schema.ResourceData) error {
	if !db.featureSupported(featurePrivileges) {
		return fmt.Errorf(
			"postgresql_grant_role resource is not supported for this Postgres version (%s)",
			db.version,
		)
	}

	return readGrantRole(db, d)
}

func resourcePostgreSQLGrantRoleCreate(db *DBConnection, d *schema.ResourceData) error {
	if !db.featureSupported(featurePrivileges) {
		return fmt.Errorf(
			"postgresql_grant_role resource is not supported for this Postgres version (%s)",
			db.version,
		)
	}

	if err := validateGrantRoleMembershipOptions(db, d); err != nil {
		return err
	}

	txn, err := startTransaction(db.client, "")
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

	return readGrantRole(db, d)
}

func resourcePostgreSQLGrantRoleDelete(db *DBConnection, d *schema.ResourceData) error {
	if !db.featureSupported(featurePrivileges) {
		return fmt.Errorf(
			"postgresql_grant_role resource is not supported for this Postgres version (%s)",
			db.version,
		)
	}

	txn, err := startTransaction(db.client, "")
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

func validateGrantRoleMembershipOptions(db *DBConnection, d *schema.ResourceData) error {
	_, inheritSet := d.GetOkExists("with_inherit_option") //nolint:staticcheck
	_, setSet := d.GetOkExists("with_set_option")         //nolint:staticcheck
	if !inheritSet && !setSet {
		return nil
	}
	if db.featureSupported(featureGrantRoleMembershipOptions) {
		return nil
	}
	return fmt.Errorf(
		"with_inherit_option and with_set_option are not supported for this Postgres version (%s); requires PostgreSQL 16+",
		db.version,
	)
}

func readGrantRole(db *DBConnection, d *schema.ResourceData) error {
	var roleName, grantRoleName string
	var withAdminOption bool

	grantRoleID := d.Id()

	if db.featureSupported(featureGrantRoleMembershipOptions) {
		var withInheritOption, withSetOption bool
		err := db.QueryRow(
			getGrantRoleQueryWithMembershipOptions,
			d.Get("role"),
			d.Get("grant_role"),
		).Scan(&roleName, &grantRoleName, &withAdminOption, &withInheritOption, &withSetOption)
		switch {
		case err == sql.ErrNoRows:
			log.Printf("[WARN] PostgreSQL grant role (%q) not found", grantRoleID)
			d.SetId("")
			return nil
		case err != nil:
			return fmt.Errorf("error reading grant role: %w", err)
		}

		d.Set("role", roleName)
		d.Set("grant_role", grantRoleName)
		d.Set("with_admin_option", withAdminOption)
		d.Set("with_inherit_option", withInheritOption)
		d.Set("with_set_option", withSetOption)
		d.SetId(generateGrantRoleID(d))
		return nil
	}

	err := db.QueryRow(getGrantRoleQuery, d.Get("role"), d.Get("grant_role")).Scan(
		&roleName,
		&grantRoleName,
		&withAdminOption,
	)
	switch {
	case err == sql.ErrNoRows:
		log.Printf("[WARN] PostgreSQL grant role (%q) not found", grantRoleID)
		d.SetId("")
		return nil
	case err != nil:
		return fmt.Errorf("error reading grant role: %w", err)
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

	var options []string
	if wao, _ := d.Get("with_admin_option").(bool); wao {
		options = append(options, "ADMIN OPTION")
	}

	if withInherit, inheritSet := d.GetOkExists("with_inherit_option"); inheritSet { //nolint:staticcheck
		options = append(options, fmt.Sprintf("INHERIT %s", boolSQL(withInherit.(bool))))
	}
	if withSet, setSet := d.GetOkExists("with_set_option"); setSet { //nolint:staticcheck
		options = append(options, fmt.Sprintf("SET %s", boolSQL(withSet.(bool))))
	}

	if len(options) > 0 {
		query = query + " WITH " + strings.Join(options, ", ")
	}

	return query
}

func boolSQL(v bool) string {
	if v {
		return "TRUE"
	}
	return "FALSE"
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
	return strings.Join([]string{
		d.Get("role").(string),
		d.Get("grant_role").(string),
		strconv.FormatBool(d.Get("with_admin_option").(bool)),
		strconv.FormatBool(d.Get("with_inherit_option").(bool)),
		strconv.FormatBool(d.Get("with_set_option").(bool)),
	}, "_")
}
