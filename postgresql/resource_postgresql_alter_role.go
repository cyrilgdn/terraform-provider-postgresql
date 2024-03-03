package postgresql

import (
	"database/sql"
	"fmt"
	"log"
	"strings"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/lib/pq"
)

const (
	getAlterRoleQuery = `
SELECT
	rolname as role,
	rolconfig as role_parameters
FROM 
	pg_catalog.pg_roles
WHERE 
	rolname = $1
`
)

func resourcePostgreSQLAlterRole() *schema.Resource {
	return &schema.Resource{
		Create: PGResourceFunc(resourcePostgreSQLAlterRoleCreate),
		Read:   PGResourceFunc(resourcePostgreSQLAlterRoleRead),
		Delete: PGResourceFunc(resourcePostgreSQLAlterRoleDelete),

		Schema: map[string]*schema.Schema{
			"role_name": {
				Type:        schema.TypeString,
				Required:    true,
				ForceNew:    true,
				Description: "The name of the role to alter the attributes of",
			},
			"parameter_key": {
				Type:        schema.TypeString,
				Required:    true,
				ForceNew:    true,
				Description: "The name of the parameter to alter on the role",
			},
			"parameter_value": {
				Type:        schema.TypeString,
				Required:    true,
				ForceNew:    true,
				Description: "The value of the parameter which is being set",
			},
		},
	}
}

func resourcePostgreSQLAlterRoleRead(db *DBConnection, d *schema.ResourceData) error {
	if !db.featureSupported(featurePrivileges) {
		return fmt.Errorf(
			"postgresql_alter_role resource is not supported for this Postgres version (%s)",
			db.version,
		)
	}

	return readAlterRole(db, d)
}

func resourcePostgreSQLAlterRoleCreate(db *DBConnection, d *schema.ResourceData) error {
	if !db.featureSupported(featurePrivileges) {
		return fmt.Errorf(
			"postgresql_alter_role resource is not supported for this Postgres version (%s)",
			db.version,
		)
	}

	txn, err := startTransaction(db.client, "")
	if err != nil {
		return err
	}
	defer deferredRollback(txn)

	// Reset the role alterations before altering them again.
	if err = resetAlterRole(txn, d); err != nil {
		return err
	}

	if err = alterRole(txn, d); err != nil {
		return err
	}

	if err = txn.Commit(); err != nil {
		return fmt.Errorf("could not commit transaction: %w", err)
	}

	d.SetId(generateAlterRoleID(d))

	return readAlterRole(db, d)
}

func resourcePostgreSQLAlterRoleDelete(db *DBConnection, d *schema.ResourceData) error {
	if !db.featureSupported(featurePrivileges) {
		return fmt.Errorf(
			"postgresql_alter_role resource is not supported for this Postgres version (%s)",
			db.version,
		)
	}

	txn, err := startTransaction(db.client, "")
	if err != nil {
		return err
	}
	defer deferredRollback(txn)

	if err = resetAlterRole(txn, d); err != nil {
		return err
	}

	if err = txn.Commit(); err != nil {
		return fmt.Errorf("could not commit transaction: %w", err)
	}

	return nil
}

func readAlterRole(db QueryAble, d *schema.ResourceData) error {
	var (
		roleName       string
		roleParameters pq.ByteaArray
	)

	alterRoleID := d.Id()
	alterParameterKey := d.Get("parameter_key")

	values := []interface{}{
		&roleName,
		&roleParameters,
	}

	err := db.QueryRow(getAlterRoleQuery, d.Get("role_name")).Scan(values...)
	switch {
	case err == sql.ErrNoRows:
		log.Printf("[WARN] PostgreSQL alter role (%q) not found", alterRoleID)
		d.SetId("")
		return nil
	case err != nil:
		return fmt.Errorf("error reading alter role: %w", err)
	}

	d.Set("parameter_key", alterParameterKey)
	d.Set("parameter_value", "")
	d.Set("role_name", roleName)
	d.SetId(generateAlterRoleID(d))

	for _, v := range roleParameters {
		parameter := string(v)
		parameterKey := strings.Split(parameter, "=")[0]
		parameterValue := strings.Split(parameter, "=")[1]
		if parameterKey == alterParameterKey {
			d.Set("parameter_key", parameterKey)
			d.Set("parameter_value", parameterValue)
		}
	}

	return nil
}

func createAlterRoleQuery(d *schema.ResourceData) string {
	alterRole, _ := d.Get("role_name").(string)
	alterParameterKey, _ := d.Get("parameter_key").(string)
	alterParameterValue, _ := d.Get("parameter_value").(string)

	query := fmt.Sprintf(
		"ALTER ROLE %s SET %s TO %s",
		pq.QuoteIdentifier(alterRole),
		pq.QuoteIdentifier(alterParameterKey),
		pq.QuoteIdentifier(alterParameterValue),
	)

	return query
}

func createResetAlterRoleQuery(d *schema.ResourceData) string {
	alterRole, _ := d.Get("role_name").(string)
	alterParameterKey, _ := d.Get("parameter_key").(string)

	return fmt.Sprintf(
		"ALTER ROLE %s RESET %s",
		pq.QuoteIdentifier(alterRole),
		pq.QuoteIdentifier(alterParameterKey),
	)
}

func alterRole(txn *sql.Tx, d *schema.ResourceData) error {
	query := createAlterRoleQuery(d)
	log.Println(query)
	if _, err := txn.Exec(query); err != nil {
		return fmt.Errorf("could not execute alter query: (%s): %w", query, err)
	}
	return nil
}

func resetAlterRole(txn *sql.Tx, d *schema.ResourceData) error {
	query := createResetAlterRoleQuery(d)
	fmt.Println(query)
	if _, err := txn.Exec(query); err != nil {
		return fmt.Errorf("could not execute alter reset query (%s): %w", query, err)
	}
	return nil
}

func generateAlterRoleID(d *schema.ResourceData) string {
	return strings.Join([]string{d.Get("role_name").(string), d.Get("parameter_key").(string), d.Get("parameter_value").(string)}, "_")
}
