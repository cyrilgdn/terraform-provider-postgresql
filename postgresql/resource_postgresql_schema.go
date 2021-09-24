package postgresql

import (
	"bytes"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"reflect"
	"strings"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/lib/pq"
	acl "github.com/sean-/postgresql-acl"
)

const (
	schemaNameAttr     = "name"
	schemaDatabaseAttr = "database"
	schemaOwnerAttr    = "owner"
	schemaPolicyAttr   = "policy"
	schemaIfNotExists  = "if_not_exists"
	schemaDropCascade  = "drop_cascade"

	schemaPolicyCreateAttr          = "create"
	schemaPolicyCreateWithGrantAttr = "create_with_grant"
	schemaPolicyRoleAttr            = "role"
	schemaPolicyUsageAttr           = "usage"
	schemaPolicyUsageWithGrantAttr  = "usage_with_grant"
)

func resourcePostgreSQLSchema() *schema.Resource {
	return &schema.Resource{
		Create: PGResourceFunc(resourcePostgreSQLSchemaCreate),
		Read:   PGResourceFunc(resourcePostgreSQLSchemaRead),
		Update: PGResourceFunc(resourcePostgreSQLSchemaUpdate),
		Delete: PGResourceFunc(resourcePostgreSQLSchemaDelete),
		Exists: PGResourceExistsFunc(resourcePostgreSQLSchemaExists),
		Importer: &schema.ResourceImporter{
			StateContext: schema.ImportStatePassthroughContext,
		},

		Schema: map[string]*schema.Schema{
			schemaNameAttr: {
				Type:        schema.TypeString,
				Required:    true,
				Description: "The name of the schema",
			},
			schemaDatabaseAttr: {
				Type:        schema.TypeString,
				Optional:    true,
				Computed:    true,
				ForceNew:    true,
				Description: "The database name to alter schema",
			},
			schemaOwnerAttr: {
				Type:        schema.TypeString,
				Optional:    true,
				Computed:    true,
				Description: "The ROLE name who owns the schema",
			},
			schemaIfNotExists: {
				Type:        schema.TypeBool,
				Optional:    true,
				Default:     true,
				Description: "When true, use the existing schema if it exists",
			},
			schemaDropCascade: {
				Type:        schema.TypeBool,
				Optional:    true,
				Default:     false,
				Description: "When true, will also drop all the objects that are contained in the schema",
			},
			schemaPolicyAttr: {
				Type:       schema.TypeSet,
				Optional:   true,
				Computed:   true,
				Deprecated: "Use postgresql_grant resource instead (with object_type=\"schema\")",
				Elem: &schema.Resource{
					Schema: map[string]*schema.Schema{
						schemaPolicyCreateAttr: {
							Type:        schema.TypeBool,
							Optional:    true,
							Default:     false,
							Description: "If true, allow the specified ROLEs to CREATE new objects within the schema(s)",
						},
						schemaPolicyCreateWithGrantAttr: {
							Type:        schema.TypeBool,
							Optional:    true,
							Default:     false,
							Description: "If true, allow the specified ROLEs to CREATE new objects within the schema(s) and GRANT the same CREATE privilege to different ROLEs",
						},
						schemaPolicyRoleAttr: {
							Type:        schema.TypeString,
							Elem:        &schema.Schema{Type: schema.TypeString},
							Optional:    true,
							Default:     "",
							Description: "ROLE who will receive this policy (default: PUBLIC)",
						},
						schemaPolicyUsageAttr: {
							Type:        schema.TypeBool,
							Optional:    true,
							Default:     false,
							Description: "If true, allow the specified ROLEs to use objects within the schema(s)",
						},
						schemaPolicyUsageWithGrantAttr: {
							Type:        schema.TypeBool,
							Optional:    true,
							Default:     false,
							Description: "If true, allow the specified ROLEs to use objects within the schema(s) and GRANT the same USAGE privilege to different ROLEs",
						},
					},
				},
			},
		},
	}
}

func resourcePostgreSQLSchemaCreate(db *DBConnection, d *schema.ResourceData) error {
	database := getDatabase(d, db.client.databaseName)
	txn, err := startTransaction(db.client, database)
	if err != nil {
		return err
	}
	defer deferredRollback(txn)

	// If the authenticated user is not a superuser (e.g. on AWS RDS)
	// we'll need to temporarily grant it membership in the following roles:
	//  * the owner of the db (to have the permissions to create the schema)
	//  * the owner of the schema, if it has one (in order to change its owner)
	var rolesToGrant []string

	dbOwner, err := getDatabaseOwner(txn, database)
	if err != nil {
		return err
	}
	rolesToGrant = append(rolesToGrant, dbOwner)

	schemaOwner := d.Get("owner").(string)
	if schemaOwner != "" && schemaOwner != dbOwner {
		rolesToGrant = append(rolesToGrant, schemaOwner)

	}

	if err := withRolesGranted(txn, rolesToGrant, func() error {
		return createSchema(db, txn, d)
	}); err != nil {
		return err
	}

	if err := txn.Commit(); err != nil {
		return fmt.Errorf("Error committing schema: %w", err)
	}

	d.SetId(generateSchemaID(d, database))

	return resourcePostgreSQLSchemaReadImpl(db, d)
}

func createSchema(db *DBConnection, txn *sql.Tx, d *schema.ResourceData) error {
	schemaName := d.Get(schemaNameAttr).(string)

	// Check if previous tasks haven't already create schema
	var foundSchema bool
	err := txn.QueryRow(`SELECT TRUE FROM pg_catalog.pg_namespace WHERE nspname = $1`, schemaName).Scan(&foundSchema)

	queries := []string{}
	switch {
	case err == sql.ErrNoRows:
		b := bytes.NewBufferString("CREATE SCHEMA ")
		if db.featureSupported(featureSchemaCreateIfNotExist) {
			if v := d.Get(schemaIfNotExists); v.(bool) {
				fmt.Fprint(b, "IF NOT EXISTS ")
			}
		}
		fmt.Fprint(b, pq.QuoteIdentifier(schemaName))

		switch v, ok := d.GetOk(schemaOwnerAttr); {
		case ok:
			fmt.Fprint(b, " AUTHORIZATION ", pq.QuoteIdentifier(v.(string)))
		}
		queries = append(queries, b.String())

	case err != nil:
		return fmt.Errorf("Error looking for schema: %w", err)

	default:
		// The schema already exists, we just set the owner.
		if err := setSchemaOwner(txn, d); err != nil {
			return err
		}
	}

	// ACL objects that can generate the necessary SQL
	type RoleKey string
	var schemaPolicies map[RoleKey]acl.Schema

	if policiesRaw, ok := d.GetOk(schemaPolicyAttr); ok {
		policiesList := policiesRaw.(*schema.Set).List()

		// NOTE: len(policiesList) doesn't take into account multiple
		// roles per policy.
		schemaPolicies = make(map[RoleKey]acl.Schema, len(policiesList))

		for _, policyRaw := range policiesList {
			policyMap := policyRaw.(map[string]interface{})
			rolePolicy := schemaPolicyToACL(policyMap)

			roleKey := RoleKey(strings.ToLower(rolePolicy.Role))
			if existingRolePolicy, ok := schemaPolicies[roleKey]; ok {
				schemaPolicies[roleKey] = existingRolePolicy.Merge(rolePolicy)
			} else {
				schemaPolicies[roleKey] = rolePolicy
			}
		}
	}

	for _, policy := range schemaPolicies {
		queries = append(queries, policy.Grants(schemaName)...)
	}

	for _, query := range queries {
		if _, err = txn.Exec(query); err != nil {
			return fmt.Errorf("Error creating schema %s: %w", schemaName, err)
		}
	}

	return nil
}

func resourcePostgreSQLSchemaDelete(db *DBConnection, d *schema.ResourceData) error {
	database := getDatabase(d, db.client.databaseName)

	txn, err := startTransaction(db.client, database)
	if err != nil {
		return err
	}
	defer deferredRollback(txn)

	schemaName := d.Get(schemaNameAttr).(string)

	exists, err := schemaExists(txn, schemaName)
	if err != nil {
		return err
	}
	if !exists {
		d.SetId("")
		return nil
	}

	owner := d.Get("owner").(string)

	if err = withRolesGranted(txn, []string{owner}, func() error {
		dropMode := "RESTRICT"
		if d.Get(schemaDropCascade).(bool) {
			dropMode = "CASCADE"
		}

		sql := fmt.Sprintf("DROP SCHEMA %s %s", pq.QuoteIdentifier(schemaName), dropMode)
		if _, err = txn.Exec(sql); err != nil {
			return fmt.Errorf("Error deleting schema: %w", err)
		}

		return nil
	}); err != nil {
		return err
	}

	if err := txn.Commit(); err != nil {
		return fmt.Errorf("Error committing schema: %w", err)
	}

	d.SetId("")

	return nil
}

func resourcePostgreSQLSchemaExists(db *DBConnection, d *schema.ResourceData) (bool, error) {
	database, schemaName, err := getDBSchemaName(d, db.client.databaseName)
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

	err = txn.QueryRow("SELECT n.nspname FROM pg_catalog.pg_namespace n WHERE n.nspname=$1", schemaName).Scan(&schemaName)
	switch {
	case err == sql.ErrNoRows:
		return false, nil
	case err != nil:
		return false, fmt.Errorf("Error reading schema: %w", err)
	}

	return true, nil
}

func resourcePostgreSQLSchemaRead(db *DBConnection, d *schema.ResourceData) error {
	return resourcePostgreSQLSchemaReadImpl(db, d)
}

func resourcePostgreSQLSchemaReadImpl(db *DBConnection, d *schema.ResourceData) error {
	database, schemaName, err := getDBSchemaName(d, db.client.databaseName)
	if err != nil {
		return err
	}

	txn, err := startTransaction(db.client, database)
	if err != nil {
		return err
	}
	defer deferredRollback(txn)

	var schemaOwner string
	var schemaACLs []string
	err = txn.QueryRow("SELECT pg_catalog.pg_get_userbyid(n.nspowner), COALESCE(n.nspacl, '{}'::aclitem[])::TEXT[] FROM pg_catalog.pg_namespace n WHERE n.nspname=$1", schemaName).Scan(&schemaOwner, pq.Array(&schemaACLs))
	switch {
	case err == sql.ErrNoRows:
		log.Printf("[WARN] PostgreSQL schema (%s) not found in database %s", schemaName, database)
		d.SetId("")
		return nil
	case err != nil:
		return fmt.Errorf("Error reading schema: %w", err)
	default:
		type RoleKey string
		schemaPolicies := make(map[RoleKey]acl.Schema, len(schemaACLs))
		for _, aclStr := range schemaACLs {
			aclItem, err := acl.Parse(aclStr)
			if err != nil {
				return fmt.Errorf("Error parsing aclitem: %w", err)
			}

			schemaACL, err := acl.NewSchema(aclItem)
			if err != nil {
				return fmt.Errorf("invalid perms for schema: %w", err)
			}

			roleKey := RoleKey(strings.ToLower(schemaACL.Role))
			var mergedPolicy acl.Schema
			if existingRolePolicy, ok := schemaPolicies[roleKey]; ok {
				mergedPolicy = existingRolePolicy.Merge(schemaACL)
			} else {
				mergedPolicy = schemaACL
			}
			schemaPolicies[roleKey] = mergedPolicy
		}

		d.Set(schemaNameAttr, schemaName)
		d.Set(schemaOwnerAttr, schemaOwner)
		d.Set(schemaDatabaseAttr, database)
		d.SetId(generateSchemaID(d, database))

		return nil
	}
}

func resourcePostgreSQLSchemaUpdate(db *DBConnection, d *schema.ResourceData) error {
	databaseName := getDatabase(d, db.client.databaseName)

	txn, err := startTransaction(db.client, databaseName)
	if err != nil {
		return err
	}
	defer deferredRollback(txn)

	if err := setSchemaName(txn, d, databaseName); err != nil {
		return err
	}

	if err := setSchemaOwner(txn, d); err != nil {
		return err
	}

	if err := setSchemaPolicy(txn, d); err != nil {
		return err
	}

	if err := txn.Commit(); err != nil {
		return fmt.Errorf("Error committing schema: %w", err)
	}

	return resourcePostgreSQLSchemaReadImpl(db, d)
}

func setSchemaName(txn *sql.Tx, d *schema.ResourceData, databaseName string) error {
	if !d.HasChange(schemaNameAttr) {
		return nil
	}

	oraw, nraw := d.GetChange(schemaNameAttr)
	o := oraw.(string)
	n := nraw.(string)
	if n == "" {
		return errors.New("Error setting schema name to an empty string")
	}

	sql := fmt.Sprintf("ALTER SCHEMA %s RENAME TO %s", pq.QuoteIdentifier(o), pq.QuoteIdentifier(n))
	if _, err := txn.Exec(sql); err != nil {
		return fmt.Errorf("Error updating schema NAME: %w", err)
	}
	d.SetId(generateSchemaID(d, databaseName))

	return nil
}

func setSchemaOwner(txn *sql.Tx, d *schema.ResourceData) error {
	if !d.HasChange(schemaOwnerAttr) {
		return nil
	}

	schemaName := d.Get(schemaNameAttr).(string)
	schemaOwner := d.Get(schemaOwnerAttr).(string)

	if schemaOwner == "" {
		return errors.New("Error setting schema owner to an empty string")
	}

	sql := fmt.Sprintf("ALTER SCHEMA %s OWNER TO %s", pq.QuoteIdentifier(schemaName), pq.QuoteIdentifier(schemaOwner))
	if _, err := txn.Exec(sql); err != nil {
		return fmt.Errorf("Error updating schema OWNER: %w", err)
	}

	return nil
}

func setSchemaPolicy(txn *sql.Tx, d *schema.ResourceData) error {
	if !d.HasChange(schemaPolicyAttr) {
		return nil
	}

	schemaName := d.Get(schemaNameAttr).(string)
	owner := d.Get(schemaOwnerAttr).(string)

	oraw, nraw := d.GetChange(schemaPolicyAttr)
	oldList := oraw.(*schema.Set).List()
	newList := nraw.(*schema.Set).List()
	queries := make([]string, 0, len(oldList)+len(newList))
	dropped, added, updated, _ := schemaChangedPolicies(oldList, newList)

	for _, p := range dropped {
		pMap := p.(map[string]interface{})
		rolePolicy := schemaPolicyToACL(pMap)

		// The PUBLIC role can not be DROP'ed, therefore we do not need
		// to prevent revoking against it not existing.
		if rolePolicy.Role != "" {
			var foundUser bool
			err := txn.QueryRow(`SELECT TRUE FROM pg_catalog.pg_roles WHERE rolname = $1`, rolePolicy.Role).Scan(&foundUser)
			switch {
			case err == sql.ErrNoRows:
				// Don't execute this role's REVOKEs because the role
				// was dropped first and therefore doesn't exist.
			case err != nil:
				return fmt.Errorf("Error reading schema: %w", err)
			default:
				queries = append(queries, rolePolicy.Revokes(schemaName)...)
			}
		}
	}

	for _, p := range added {
		pMap := p.(map[string]interface{})
		rolePolicy := schemaPolicyToACL(pMap)
		queries = append(queries, rolePolicy.Grants(schemaName)...)
	}

	for _, p := range updated {
		policies := p.([]interface{})
		if len(policies) != 2 {
			panic("expected 2 policies, old and new")
		}

		{
			oldPolicies := policies[0].(map[string]interface{})
			rolePolicy := schemaPolicyToACL(oldPolicies)
			queries = append(queries, rolePolicy.Revokes(schemaName)...)
		}

		{
			newPolicies := policies[1].(map[string]interface{})
			rolePolicy := schemaPolicyToACL(newPolicies)
			queries = append(queries, rolePolicy.Grants(schemaName)...)
		}
	}

	rolesToGrant := []string{}
	if owner != "" {
		rolesToGrant = append(rolesToGrant, owner)
	}

	return withRolesGranted(txn, rolesToGrant, func() error {
		for _, query := range queries {
			if _, err := txn.Exec(query); err != nil {
				return fmt.Errorf("Error updating schema DCL: %w", err)
			}
		}
		return nil
	})
}

// schemaChangedPolicies walks old and new to create a set of queries that can
// be executed to enact each type of state change (roles that have been dropped
// from the policy, added to a policy, have updated privilges, or are
// unchanged).
func schemaChangedPolicies(old, new []interface{}) (dropped, added, update, unchanged map[string]interface{}) {
	type RoleKey string
	oldLookupMap := make(map[RoleKey]interface{}, len(old))
	for idx := range old {
		v := old[idx]
		schemaPolicy := v.(map[string]interface{})
		if roleRaw, ok := schemaPolicy[schemaPolicyRoleAttr]; ok {
			role := roleRaw.(string)
			roleKey := strings.ToLower(role)
			oldLookupMap[RoleKey(roleKey)] = schemaPolicy
		}
	}

	newLookupMap := make(map[RoleKey]interface{}, len(new))
	for idx := range new {
		v := new[idx]
		schemaPolicy := v.(map[string]interface{})
		if roleRaw, ok := schemaPolicy[schemaPolicyRoleAttr]; ok {
			role := roleRaw.(string)
			roleKey := strings.ToLower(role)
			newLookupMap[RoleKey(roleKey)] = schemaPolicy
		}
	}

	droppedRoles := make(map[string]interface{}, len(old))
	for kOld, vOld := range oldLookupMap {
		if _, ok := newLookupMap[kOld]; !ok {
			droppedRoles[string(kOld)] = vOld
		}
	}

	addedRoles := make(map[string]interface{}, len(new))
	for kNew, vNew := range newLookupMap {
		if _, ok := oldLookupMap[kNew]; !ok {
			addedRoles[string(kNew)] = vNew
		}
	}

	updatedRoles := make(map[string]interface{}, len(new))
	unchangedRoles := make(map[string]interface{}, len(new))
	for kOld, vOld := range oldLookupMap {
		if vNew, ok := newLookupMap[kOld]; ok {
			if reflect.DeepEqual(vOld, vNew) {
				unchangedRoles[string(kOld)] = vOld
			} else {
				updatedRoles[string(kOld)] = []interface{}{vOld, vNew}
			}
		}
	}

	return droppedRoles, addedRoles, updatedRoles, unchangedRoles
}

func schemaPolicyToACL(policyMap map[string]interface{}) acl.Schema {
	var rolePolicy acl.Schema

	if policyMap[schemaPolicyCreateAttr].(bool) {
		rolePolicy.Privileges |= acl.Create
	}

	if policyMap[schemaPolicyCreateWithGrantAttr].(bool) {
		rolePolicy.Privileges |= acl.Create
		rolePolicy.GrantOptions |= acl.Create
	}

	if policyMap[schemaPolicyUsageAttr].(bool) {
		rolePolicy.Privileges |= acl.Usage
	}

	if policyMap[schemaPolicyUsageWithGrantAttr].(bool) {
		rolePolicy.Privileges |= acl.Usage
		rolePolicy.GrantOptions |= acl.Usage
	}

	if roleRaw, ok := policyMap[schemaPolicyRoleAttr]; ok {
		rolePolicy.Role = roleRaw.(string)
	}

	return rolePolicy
}

func generateSchemaID(d *schema.ResourceData, databaseName string) string {
	SchemaID := strings.Join([]string{
		getDatabase(d, databaseName),
		d.Get(schemaNameAttr).(string),
	}, ".")

	return SchemaID
}

func getDBSchemaName(d *schema.ResourceData, databaseName string) (string, string, error) {
	database := getDatabase(d, databaseName)
	schemaName := d.Get(schemaNameAttr).(string)

	// When importing, we have to parse the ID to find schema and database names.
	if schemaName == "" {
		parsed := strings.Split(d.Id(), ".")
		if len(parsed) != 2 {
			return "", "", fmt.Errorf("schema ID %s has not the expected format 'database.schema': %v", d.Id(), parsed)
		}
		database = parsed[0]
		schemaName = parsed[1]
	}
	return database, schemaName, nil
}
