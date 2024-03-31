package postgresql

import (
	"database/sql"
	"fmt"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/lib/pq"
)

func resourcePostgreSQLPermissions() *schema.Resource {
	return &schema.Resource{
		Create: PGResourceFunc(resourcePostgreSQLPermissionsCreate),
		Read:   PGResourceFunc(resourcePostgreSQLPermissionsRead),
		Update: PGResourceFunc(resourcePostgreSQLPermissionsUpdate),
		Delete: PGResourceFunc(resourcePostgreSQLPermissionsDelete),

		Schema: map[string]*schema.Schema{
			"role": {
				Type:     schema.TypeString,
				Required: true,
				ForceNew: true,
			},
			"create_db": {
				Type:     schema.TypeBool,
				Optional: true,
				Default:  false,
			},
			"create_role": {
				Type:     schema.TypeBool,
				Optional: true,
				Default:  false,
			},
		},
	}
}

func resourcePostgreSQLPermissionsRead(db *DBConnection, d *schema.ResourceData) error {
	roleName := d.Get("role").(string)

	var rolname string
	var rolcreatedb, rolcreaterole bool

	err := db.QueryRow(`SELECT rolname, rolcreatedb, rolcreaterole FROM pg_roles WHERE rolname = $1;`, roleName).Scan(&rolname, &rolcreatedb, &rolcreaterole)
	if err != nil {
		if err == sql.ErrNoRows {
			d.SetId("")
			return nil
		}
		return err
	}

	d.Set("role", rolname)
	d.Set("create_db", rolcreatedb)
	d.Set("create_role", rolcreaterole)

	d.SetId(rolname)

	return nil
}

func resourcePostgreSQLPermissionsCreate(db *DBConnection, d *schema.ResourceData) error {
	roleName := d.Get("role").(string)
	createDb := d.Get("create_db").(bool)
	createRole := d.Get("create_role").(bool)

	var queries []string
	// Remove the role from the database if it already exists
	queries = append(queries, fmt.Sprintf("ALTER ROLE %s NOCREATEDB;", pq.QuoteIdentifier(roleName)))
	queries = append(queries, fmt.Sprintf("ALTER ROLE %s NOCREATEROLE;", pq.QuoteIdentifier(roleName)))
	if createDb {
		queries = append(queries, fmt.Sprintf("ALTER ROLE %s CREATEDB;", pq.QuoteIdentifier(roleName)))
	}
	if createRole {
		queries = append(queries, fmt.Sprintf("ALTER ROLE %s CREATEROLE;", pq.QuoteIdentifier(roleName)))
	}

	for _, query := range queries {
		if _, err := db.Exec(query); err != nil {
			return fmt.Errorf("error executing SQL query: %s, error: %w", query, err)
		}
	}

	d.SetId(roleName)

	return resourcePostgreSQLPermissionsRead(db, d)
}

func resourcePostgreSQLPermissionsDelete(db *DBConnection, d *schema.ResourceData) error {
	roleName := d.Get("role").(string)
	createDb := d.Get("create_db").(bool)
	createRole := d.Get("create_role").(bool)

	if createDb {
		if _, err := db.Exec(fmt.Sprintf("ALTER ROLE %s NOCREATEDB;", pq.QuoteIdentifier(roleName))); err != nil {
			return err
		}
	}
	if createRole {
		if _, err := db.Exec(fmt.Sprintf("ALTER ROLE %s NOCREATEROLE;", pq.QuoteIdentifier(roleName))); err != nil {
			return err
		}
	}

	d.SetId("")

	return nil
}

func resourcePostgreSQLPermissionsUpdate(db *DBConnection, d *schema.ResourceData) error {
	if d.HasChange("create_db") || d.HasChange("create_role") {
		roleName := d.Get("role").(string)
		desiredCreateDb := d.Get("create_db").(bool)
		desiredCreateRole := d.Get("create_role").(bool)

		// Fetch the current state
		var currentCreateDb, currentCreateRole bool
		err := db.QueryRow(`SELECT rolcreatedb, rolcreaterole FROM pg_roles WHERE rolname = $1;`, roleName).Scan(&currentCreateDb, &currentCreateRole)
		if err != nil {
			return err
		}

		// Compare and update CREATE_DB permission if needed
		if desiredCreateDb != currentCreateDb {
			if desiredCreateDb {
				if _, err := db.Exec(fmt.Sprintf("ALTER ROLE %s CREATEDB;", pq.QuoteIdentifier(roleName))); err != nil {
					return fmt.Errorf("error granting CREATEDB to role %s: %w", roleName, err)
				}
			} else {
				if _, err := db.Exec(fmt.Sprintf("ALTER ROLE %s NOCREATEDB;", pq.QuoteIdentifier(roleName))); err != nil {
					return fmt.Errorf("error revoking CREATEDB from role %s: %w", roleName, err)
				}
			}
		}

		// Compare and update CREATE_ROLE permission if needed
		if desiredCreateRole != currentCreateRole {
			if desiredCreateRole {
				if _, err := db.Exec(fmt.Sprintf("ALTER ROLE %s CREATEROLE;", pq.QuoteIdentifier(roleName))); err != nil {
					return fmt.Errorf("error granting CREATEROLE to role %s: %w", roleName, err)
				}
			} else {
				if _, err := db.Exec(fmt.Sprintf("ALTER ROLE %s NOCREATEROLE;", pq.QuoteIdentifier(roleName))); err != nil {
					return fmt.Errorf("error revoking CREATEROLE from role %s: %w", roleName, err)
				}
			}
		}
	}

	return resourcePostgreSQLPermissionsRead(db, d)
}
