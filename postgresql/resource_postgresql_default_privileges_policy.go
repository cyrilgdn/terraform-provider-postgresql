package postgresql

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"sort"
	"strings"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/validation"

	// Use Postgres as SQL driver
	"github.com/lib/pq"
)

// defaultPrivilegesPolicyObjectTypes is the set of object types supported by
// postgresql_default_privileges_policy. It intentionally excludes "routine"
// (an alias of "function" at the catalog level) to keep the reverse mapping
// from pg_default_acl unambiguous.
var defaultPrivilegesPolicyObjectTypes = []string{
	"table",
	"sequence",
	"function",
	"type",
	"schema",
}

var defaultPrivilegesPolicyObjectTypeByAclChar = map[string]string{
	"r": "table",
	"S": "sequence",
	"f": "function",
	"T": "type",
	"n": "schema",
}

// defaultPrivilegesGrantKey identifies a single (schema, object_type, role) grant
// within a postgresql_default_privileges_policy resource. schema is "" for
// database-wide (global) default privileges.
type defaultPrivilegesGrantKey struct {
	schema     string
	objectType string
	role       string
}

type defaultPrivilegesGrantValue struct {
	privileges      *schema.Set
	withGrantOption bool
}

type currentDefaultPrivilegeGrant struct {
	key             defaultPrivilegesGrantKey
	privileges      *schema.Set
	withGrantOption bool
}

func resourcePostgreSQLDefaultPrivilegesPolicy() *schema.Resource {
	return &schema.Resource{
		Create: PGResourceFunc(resourcePostgreSQLDefaultPrivilegesPolicyCreateOrUpdate),
		Update: PGResourceFunc(resourcePostgreSQLDefaultPrivilegesPolicyCreateOrUpdate),
		Read:   PGResourceFunc(resourcePostgreSQLDefaultPrivilegesPolicyRead),
		Delete: PGResourceFunc(resourcePostgreSQLDefaultPrivilegesPolicyDelete),
		Importer: &schema.ResourceImporter{
			StateContext: resourcePostgreSQLDefaultPrivilegesPolicyImport,
		},

		Schema: map[string]*schema.Schema{
			"database": {
				Type:        schema.TypeString,
				Required:    true,
				ForceNew:    true,
				Description: "The database to manage default privileges for this owner",
			},
			"owner": {
				Type:        schema.TypeString,
				Required:    true,
				ForceNew:    true,
				Description: "Target role for which to alter default privileges.",
			},
			"authoritative": {
				Type:     schema.TypeBool,
				Optional: true,
				Default:  false,
				Description: "If true, this resource takes full ownership of every default privilege granted by " +
					"`owner` in `database` for the object types it manages (table, sequence, function, type, schema): " +
					"anything found in Postgres that is not declared in `grants` is REVOKED on every apply and on " +
					"destroy. If false (default), the resource only ensures the grants declared in `grants` are " +
					"present (additive mode) and never revokes privileges it did not declare.",
			},
			"grants": {
				Type:        schema.TypeList,
				Required:    true,
				Description: "The list of default privilege grants to apply for this (database, owner) policy",
				// Without this, Terraform exposes a TypeList of *schema.Resource only as a
				// repeated block (grants { ... }), not as grants = [ { ... } ] — every piece of
				// documentation/tests/examples for this resource use attribute syntax, so this
				// must render as an attribute. Caught by a real `terraform validate` against a
				// real caller, not by any of this repo's own tests (none of them actually parse
				// HCL through the real Terraform CLI).
				ConfigMode: schema.SchemaConfigModeAttr,
				Elem: &schema.Resource{
					Schema: map[string]*schema.Schema{
						"schema": {
							Type:        schema.TypeString,
							Optional:    true,
							Description: "The database schema to set default privileges for this role. If omitted, applies globally to the database",
						},
						"role": {
							Type:        schema.TypeString,
							Required:    true,
							Description: "The role that will automatically be granted the specified privileges on new objects created by the owner",
						},
						"object_type": {
							Type:         schema.TypeString,
							Required:     true,
							ValidateFunc: validation.StringInSlice(defaultPrivilegesPolicyObjectTypes, false),
							Description:  "The PostgreSQL object type to set the default privileges on (one of: " + strings.Join(defaultPrivilegesPolicyObjectTypes, ", ") + ")",
						},
						"privileges": {
							Type:        schema.TypeSet,
							Required:    true,
							Elem:        &schema.Schema{Type: schema.TypeString},
							Set:         schema.HashString,
							Description: "The list of privileges to apply as default privileges",
						},
						"with_grant_option": {
							Type:        schema.TypeBool,
							Optional:    true,
							Default:     false,
							Description: "Permit the grant recipient to grant it to others",
						},
					},
				},
			},
			"managed_grants": {
				Type:     schema.TypeList,
				Computed: true,
				Description: "Internal bookkeeping: exactly the grants this resource itself applied on its most " +
					"recent Create/Update, nothing else. Read and Import never write this field, specifically so " +
					"that grants merely observed in Postgres (e.g. defaults set by an external SQL script for a " +
					"role never declared in `grants`) can never end up here. When an entry is removed from " +
					"`grants`, this is what `authoritative = false` diffs against to decide what to revoke: only " +
					"things this resource actually granted before, never anything it only ever observed.",
				Elem: &schema.Resource{
					Schema: map[string]*schema.Schema{
						"schema":            {Type: schema.TypeString, Computed: true},
						"role":              {Type: schema.TypeString, Computed: true},
						"object_type":       {Type: schema.TypeString, Computed: true},
						"privileges":        {Type: schema.TypeSet, Computed: true, Elem: &schema.Schema{Type: schema.TypeString}, Set: schema.HashString},
						"with_grant_option": {Type: schema.TypeBool, Computed: true},
					},
				},
			},
		},
	}
}

func resourcePostgreSQLDefaultPrivilegesPolicyRead(db *DBConnection, d *schema.ResourceData) error {
	if !db.featureSupported(featurePrivileges) {
		return fmt.Errorf(
			"postgresql_default_privileges_policy resource is not supported for this Postgres version (%s)",
			db.version,
		)
	}

	database := d.Get("database").(string)
	owner := d.Get("owner").(string)

	exists, err := dbExists(db, database)
	if err != nil {
		return err
	}
	if !exists {
		log.Printf("[DEBUG] database %s does not exist", database)
		d.SetId("")
		return nil
	}

	txn, err := startTransaction(db.client, database)
	if err != nil {
		return err
	}
	defer deferredRollback(txn)

	exists, err = roleExists(txn, owner)
	if err != nil {
		return err
	}
	if !exists {
		log.Printf("[DEBUG] owner role %s does not exist", owner)
		d.SetId("")
		return nil
	}

	return readDefaultPrivilegesPolicy(txn, d)
}

func resourcePostgreSQLDefaultPrivilegesPolicyCreateOrUpdate(db *DBConnection, d *schema.ResourceData) error {
	if !db.featureSupported(featurePrivileges) {
		return fmt.Errorf(
			"postgresql_default_privileges_policy resource is not supported for this Postgres version (%s)",
			db.version,
		)
	}

	grants, err := expandDefaultPrivilegesPolicyGrants(d)
	if err != nil {
		return err
	}
	if err := validateDefaultPrivilegesPolicyGrants(db, grants); err != nil {
		return err
	}

	// Must read before any d.Set call in this apply touches it.
	previouslyManaged := expandManagedGrantKeys(d)

	database := d.Get("database").(string)
	owner := d.Get("owner").(string)
	authoritative := d.Get("authoritative").(bool)

	txn, err := startTransaction(db.client, database)
	if err != nil {
		return err
	}
	defer deferredRollback(txn)

	if err := pgLockRole(txn, owner); err != nil {
		return err
	}

	// Needed in order to alter default privileges if the connection user is not a superuser
	if err := withRolesGranted(txn, []string{owner}, func() error {
		if authoritative {
			current, err := readCurrentDefaultPrivilegesPolicyGrants(txn, owner)
			if err != nil {
				return err
			}
			for _, cur := range current {
				if _, declared := grants[cur.key]; declared {
					continue
				}
				if err := revokeDefaultPrivilegesPolicyGrant(txn, owner, cur.key); err != nil {
					return err
				}
			}
		} else {
			// Explicitly revoke entries this resource itself granted before that are no
			// longer declared. previouslyManaged only ever contains grants this resource
			// applied via a prior Create/Update (never anything merely observed by Read or
			// Import), so this can't touch unmanaged drift like a role's defaults set by an
			// external SQL script.
			for key := range previouslyManaged {
				if _, declared := grants[key]; declared {
					continue
				}
				if err := revokeDefaultPrivilegesPolicyGrant(txn, owner, key); err != nil {
					return err
				}
			}
		}

		// Sort for deterministic statement ordering (helps reading logs/errors).
		keys := make([]defaultPrivilegesGrantKey, 0, len(grants))
		for key := range grants {
			keys = append(keys, key)
		}
		sort.Slice(keys, func(i, j int) bool {
			return grantKeyLess(keys[i], keys[j])
		})

		for _, key := range keys {
			// Revoke all privileges before granting otherwise reducing privileges will not work.
			// We just have to revoke them in the same transaction so the role will not lose its
			// privileges between the revoke and grant statements.
			if err := revokeDefaultPrivilegesPolicyGrant(txn, owner, key); err != nil {
				return err
			}
			if err := grantDefaultPrivilegesPolicyGrant(txn, owner, key, grants[key]); err != nil {
				return err
			}
		}

		return nil
	}); err != nil {
		return err
	}

	if err := txn.Commit(); err != nil {
		return fmt.Errorf("could not commit transaction: %w", err)
	}

	d.SetId(generateDefaultPrivilegesPolicyID(database, owner))

	// Record exactly what was just applied as the new ledger, before Read runs.
	if err := setManagedGrants(d, grants); err != nil {
		return err
	}

	readTxn, err := startTransaction(db.client, database)
	if err != nil {
		return err
	}
	defer deferredRollback(readTxn)

	return readDefaultPrivilegesPolicy(readTxn, d)
}

func resourcePostgreSQLDefaultPrivilegesPolicyDelete(db *DBConnection, d *schema.ResourceData) error {
	database := d.Get("database").(string)
	owner := d.Get("owner").(string)
	authoritative := d.Get("authoritative").(bool)

	txn, err := startTransaction(db.client, database)
	if err != nil {
		return err
	}
	defer deferredRollback(txn)

	if err := pgLockRole(txn, owner); err != nil {
		return err
	}

	if err := withRolesGranted(txn, []string{owner}, func() error {
		if authoritative {
			// The policy owns the whole set of default privileges for this owner:
			// clean up everything found, not just what is declared in `grants`.
			current, err := readCurrentDefaultPrivilegesPolicyGrants(txn, owner)
			if err != nil {
				return err
			}
			for _, cur := range current {
				if err := revokeDefaultPrivilegesPolicyGrant(txn, owner, cur.key); err != nil {
					return err
				}
			}
			return nil
		}

		// Additive mode: only revoke what this resource actually granted — the same
		// managed_grants ledger Create/Update maintains, not merely what's in the
		// (about-to-be-destroyed) config, so this stays correct even if a prior apply
		// partially failed.
		for key := range expandManagedGrantKeys(d) {
			if err := revokeDefaultPrivilegesPolicyGrant(txn, owner, key); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return err
	}

	if err := txn.Commit(); err != nil {
		return fmt.Errorf("could not commit transaction: %w", err)
	}

	return nil
}

// expandDefaultPrivilegesPolicyGrants parses and validates the `grants` block
// from the resource config into a map keyed by (schema, object_type, role).
func expandDefaultPrivilegesPolicyGrants(d *schema.ResourceData) (map[defaultPrivilegesGrantKey]defaultPrivilegesGrantValue, error) {
	raw := d.Get("grants").([]interface{})
	grants := make(map[defaultPrivilegesGrantKey]defaultPrivilegesGrantValue, len(raw))

	for _, item := range raw {
		grant := item.(map[string]interface{})

		key := defaultPrivilegesGrantKey{
			schema:     grant["schema"].(string),
			objectType: grant["object_type"].(string),
			role:       grant["role"].(string),
		}

		if _, exists := grants[key]; exists {
			return nil, fmt.Errorf(
				"duplicate grant for role %q, object_type %q, schema %q: each (schema, role, object_type) combination must appear only once in `grants`",
				key.role, key.objectType, key.schema,
			)
		}

		withGrantOption := grant["with_grant_option"].(bool)
		if withGrantOption && strings.ToLower(key.role) == publicRole {
			return nil, fmt.Errorf("with_grant_option cannot be true for role 'public'")
		}

		grants[key] = defaultPrivilegesGrantValue{
			privileges:      grant["privileges"].(*schema.Set),
			withGrantOption: withGrantOption,
		}
	}

	return grants, nil
}

// expandManagedGrantKeys parses the managed_grants ledger into a set of keys.
// Only Create/Update ever write this field (see setManagedGrants), so unlike
// `grants`, it can never contain a key that came from an Import/Read snapshot
// of live Postgres state that this resource didn't itself grant.
func expandManagedGrantKeys(d *schema.ResourceData) map[defaultPrivilegesGrantKey]bool {
	raw := d.Get("managed_grants").([]interface{})
	keys := make(map[defaultPrivilegesGrantKey]bool, len(raw))
	for _, item := range raw {
		entry := item.(map[string]interface{})
		keys[defaultPrivilegesGrantKey{
			schema:     entry["schema"].(string),
			objectType: entry["object_type"].(string),
			role:       entry["role"].(string),
		}] = true
	}
	return keys
}

// setManagedGrants overwrites the managed_grants ledger with exactly what was
// just applied. Called only from Create/Update, after a successful commit.
func setManagedGrants(d *schema.ResourceData, grants map[defaultPrivilegesGrantKey]defaultPrivilegesGrantValue) error {
	managed := make([]interface{}, 0, len(grants))
	for key, value := range grants {
		managed = append(managed, map[string]interface{}{
			"schema":            key.schema,
			"role":              key.role,
			"object_type":       key.objectType,
			"privileges":        value.privileges,
			"with_grant_option": value.withGrantOption,
		})
	}
	if err := d.Set("managed_grants", managed); err != nil {
		return fmt.Errorf("could not set managed_grants: %w", err)
	}
	return nil
}

// validateDefaultPrivilegesPolicyGrants checks version support and allowed
// privileges for every declared grant.
func validateDefaultPrivilegesPolicyGrants(db *DBConnection, grants map[defaultPrivilegesGrantKey]defaultPrivilegesGrantValue) error {
	for key, value := range grants {
		if key.schema != "" && key.objectType == "schema" {
			if !db.featureSupported(featurePrivilegesOnSchemas) {
				return fmt.Errorf(
					"changing default privileges for schemas is not supported for this Postgres version (%s)",
					db.version,
				)
			}
			return fmt.Errorf("cannot specify `schema` when `object_type` is `schema`")
		}

		allowed := allowedPrivilegesForObjectType(key.objectType, db)
		if allowed == nil {
			return fmt.Errorf("unknown object type %s", key.objectType)
		}
		for _, priv := range value.privileges.List() {
			if !sliceContainsStr(allowed, priv.(string)) {
				return fmt.Errorf("%s is not an allowed privilege for object type %s", priv, key.objectType)
			}
		}
	}
	return nil
}

func grantKeyLess(a, b defaultPrivilegesGrantKey) bool {
	if a.schema != b.schema {
		return a.schema < b.schema
	}
	if a.objectType != b.objectType {
		return a.objectType < b.objectType
	}
	return a.role < b.role
}

func inSchemaClause(pgSchema string) string {
	if pgSchema == "" {
		return ""
	}
	return fmt.Sprintf("IN SCHEMA %s", pq.QuoteIdentifier(pgSchema))
}

func revokeDefaultPrivilegesPolicyGrant(txn *sql.Tx, owner string, key defaultPrivilegesGrantKey) error {
	query := fmt.Sprintf(
		"ALTER DEFAULT PRIVILEGES FOR ROLE %s %s REVOKE ALL ON %sS FROM %s",
		pq.QuoteIdentifier(owner),
		inSchemaClause(key.schema),
		strings.ToUpper(key.objectType),
		pq.QuoteIdentifier(key.role),
	)

	if _, err := txn.Exec(query); err != nil {
		return fmt.Errorf("could not revoke default privileges: %w", err)
	}
	return nil
}

func grantDefaultPrivilegesPolicyGrant(txn *sql.Tx, owner string, key defaultPrivilegesGrantKey, value defaultPrivilegesGrantValue) error {
	privileges := make([]string, 0, value.privileges.Len())
	for _, priv := range value.privileges.List() {
		privileges = append(privileges, priv.(string))
	}

	if len(privileges) == 0 {
		log.Printf("[DEBUG] no default privileges to grant for role %s, owner %s", key.role, owner)
		return nil
	}

	query := fmt.Sprintf(
		"ALTER DEFAULT PRIVILEGES FOR ROLE %s %s GRANT %s ON %sS TO %s",
		pq.QuoteIdentifier(owner),
		inSchemaClause(key.schema),
		strings.Join(privileges, ","),
		strings.ToUpper(key.objectType),
		pq.QuoteIdentifier(key.role),
	)

	if value.withGrantOption {
		query += " WITH GRANT OPTION"
	}

	if _, err := txn.Exec(query); err != nil {
		return fmt.Errorf("could not alter default privileges: %w", err)
	}
	return nil
}

// readCurrentDefaultPrivilegesPolicyGrants returns the full set of default
// privileges currently recorded in pg_default_acl for the given owner,
// across every schema (including the database-wide/global entry).
func readCurrentDefaultPrivilegesPolicyGrants(txn *sql.Tx, owner string) ([]currentDefaultPrivilegeGrant, error) {
	ownerOID, err := getRoleOID(txn, owner)
	if err != nil {
		return nil, err
	}

	query := `
SELECT
	pg_namespace.nspname,
	t.objtype,
	pg_get_userbyid(t.grantee) AS grantee,
	array_agg(t.prtype) AS privileges,
	bool_and(t.grantable) AS with_grant_option
FROM (
	SELECT defaclnamespace AS namespace, defaclobjtype AS objtype, (aclexplode(defaclacl)).*
	FROM pg_default_acl
	WHERE defaclrole = $1
) AS t (namespace, objtype, grantor, grantee, prtype, grantable)
LEFT JOIN pg_namespace ON pg_namespace.oid = t.namespace
GROUP BY pg_namespace.nspname, t.objtype, grantee
`

	rows, err := txn.Query(query, ownerOID)
	if err != nil {
		return nil, fmt.Errorf("could not read default privileges for owner %s: %w", owner, err)
	}
	defer rows.Close()

	var grants []currentDefaultPrivilegeGrant
	for rows.Next() {
		var schemaName sql.NullString
		var aclObjType string
		var grantee string
		var privileges pq.ByteaArray
		var withGrantOption bool

		if err := rows.Scan(&schemaName, &aclObjType, &grantee, &privileges, &withGrantOption); err != nil {
			return nil, fmt.Errorf("could not scan default privileges row: %w", err)
		}

		objectType, ok := defaultPrivilegesPolicyObjectTypeByAclChar[aclObjType]
		if !ok {
			// Object type not managed by this resource (e.g. defaults set as
			// "routine" by postgresql_default_privileges); leave it alone.
			continue
		}

		grants = append(grants, currentDefaultPrivilegeGrant{
			key: defaultPrivilegesGrantKey{
				schema:     schemaName.String,
				objectType: objectType,
				role:       grantee,
			},
			privileges:      pgArrayToSet(privileges),
			withGrantOption: withGrantOption,
		})
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return grants, nil
}

// readDefaultPrivilegesPolicy refreshes the `grants` privileges/with_grant_option
// for every (schema, object_type, role) entry already declared in state,
// reflecting drift that happened outside of Terraform.
func readDefaultPrivilegesPolicy(txn *sql.Tx, d *schema.ResourceData) error {
	database := d.Get("database").(string)
	owner := d.Get("owner").(string)
	declaredRaw := d.Get("grants").([]interface{})

	current, err := readCurrentDefaultPrivilegesPolicyGrants(txn, owner)
	if err != nil {
		return err
	}

	currentByKey := make(map[defaultPrivilegesGrantKey]currentDefaultPrivilegeGrant, len(current))
	for _, grant := range current {
		currentByKey[grant.key] = grant
	}

	grants := make([]interface{}, 0, len(declaredRaw))
	for _, item := range declaredRaw {
		declared := item.(map[string]interface{})

		key := defaultPrivilegesGrantKey{
			schema:     declared["schema"].(string),
			objectType: declared["object_type"].(string),
			role:       declared["role"].(string),
		}

		privileges := schema.NewSet(schema.HashString, nil)
		withGrantOption := false
		if actual, ok := currentByKey[key]; ok {
			privileges = actual.privileges
			withGrantOption = actual.withGrantOption
		} else {
			log.Printf(
				"[DEBUG] no default privileges found for role %s, owner %s, schema %q, object_type %s",
				key.role, owner, key.schema, key.objectType,
			)
		}

		grants = append(grants, map[string]interface{}{
			"schema":            key.schema,
			"role":              key.role,
			"object_type":       key.objectType,
			"privileges":        privileges,
			"with_grant_option": withGrantOption,
		})
	}

	if err := d.Set("grants", grants); err != nil {
		return fmt.Errorf("could not set grants: %w", err)
	}

	d.SetId(generateDefaultPrivilegesPolicyID(database, owner))

	return nil
}

func generateDefaultPrivilegesPolicyID(database, owner string) string {
	return strings.Join([]string{database, owner}, "_")
}

// resourcePostgreSQLDefaultPrivilegesPolicyImport parses an import ID formatted as
// "<database>/<owner>", fetches every default privilege currently recorded in
// pg_default_acl for that owner and seeds `grants` with the full result: on
// import there is no prior declared list to diff against, so every grant found
// for the object types this resource manages is considered part of the policy.
func resourcePostgreSQLDefaultPrivilegesPolicyImport(ctx context.Context, d *schema.ResourceData, meta any) ([]*schema.ResourceData, error) {
	id := d.Id()
	parts := strings.SplitN(id, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return nil, fmt.Errorf(
			"invalid ID %q for postgresql_default_privileges_policy: expected format '<database>/<owner>'",
			id,
		)
	}
	database, owner := parts[0], parts[1]

	client := meta.(*Client)
	db, err := client.Connect()
	if err != nil {
		return nil, err
	}

	if !db.featureSupported(featurePrivileges) {
		return nil, fmt.Errorf(
			"postgresql_default_privileges_policy resource is not supported for this Postgres version (%s)",
			db.version,
		)
	}

	exists, err := dbExists(db, database)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, fmt.Errorf("database %q does not exist", database)
	}

	txn, err := startTransaction(db.client, database)
	if err != nil {
		return nil, err
	}
	defer deferredRollback(txn)

	exists, err = roleExists(txn, owner)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, fmt.Errorf("owner role %q does not exist", owner)
	}

	current, err := readCurrentDefaultPrivilegesPolicyGrants(txn, owner)
	if err != nil {
		return nil, err
	}

	sort.Slice(current, func(i, j int) bool {
		return grantKeyLess(current[i].key, current[j].key)
	})

	grants := make([]interface{}, 0, len(current))
	for _, grant := range current {
		grants = append(grants, map[string]interface{}{
			"schema":            grant.key.schema,
			"role":              grant.key.role,
			"object_type":       grant.key.objectType,
			"privileges":        grant.privileges,
			"with_grant_option": grant.withGrantOption,
		})
	}

	if err := d.Set("database", database); err != nil {
		return nil, err
	}
	if err := d.Set("owner", owner); err != nil {
		return nil, err
	}
	// authoritative cannot be inferred from Postgres state; default to the
	// safe additive value so an import never becomes destructive by accident.
	if err := d.Set("authoritative", false); err != nil {
		return nil, err
	}
	if err := d.Set("grants", grants); err != nil {
		return nil, err
	}

	d.SetId(generateDefaultPrivilegesPolicyID(database, owner))

	return []*schema.ResourceData{d}, nil
}
