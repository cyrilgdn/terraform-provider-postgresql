package postgresql

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/validation"

	// Use Postgres as SQL driver
	"github.com/lib/pq"
)

// grantPolicyObjectTypes is the set of object types supported by
// postgresql_grant_policy. It is a subset of postgresql_grant's
// allowedObjectTypes, excluding:
//   - "column": keyed by (table, column, single privilege), which does not
//     fit the (schema, object_type) grant-entry shape every other object
//     type here shares.
//   - "routine": an alias of "function"/"procedure" at the catalog level
//     (aclexplode has no dedicated "routine" tag), the same exclusion
//     postgresql_default_privileges_policy already makes for the same
//     reason (see defaultPrivilegesPolicyObjectTypes).
//   - "type": postgresql_grant validates it but never actually implements a
//     GRANT/REVOKE query for it (createGrantQuery/createRevokeQuery have no
//     "TYPE" case), so there is nothing meaningful to mirror here.
var grantPolicyObjectTypes = []string{
	"database",
	"schema",
	"table",
	"sequence",
	"function",
	"procedure",
	"foreign_data_wrapper",
	"foreign_server",
}

// grantPolicySchemaScopedObjectTypes are object types where the `schema`
// field on a grant entry is required and denotes the schema *containing*
// the objects (as opposed to "schema", where it denotes the target itself).
var grantPolicySchemaScopedObjectTypes = []string{"table", "sequence", "function", "procedure"}

// grantPolicyNoSchemaObjectTypes are object types where `schema` must be empty.
var grantPolicyNoSchemaObjectTypes = []string{"database", "foreign_data_wrapper", "foreign_server"}

// grantPolicyExplicitObjectTypes are object types that always require at
// least one entry in `objects` (there is no "ALL <TYPE>S" grant form for
// them in Postgres).
var grantPolicyExplicitObjectTypes = []string{"foreign_data_wrapper", "foreign_server"}

// grantPolicyGrantKey identifies a single grant entry within a
// postgresql_grant_policy resource: `schema` is "" for object types in
// grantPolicyNoSchemaObjectTypes, and is the target schema name itself when
// object_type is "schema" (as opposed to the containing schema for
// table/sequence/function/procedure).
type grantPolicyGrantKey struct {
	schema     string
	objectType string
}

type grantPolicyGrantValue struct {
	objects         *schema.Set // specific object names; empty means "every object of this type in schema/database"
	privileges      *schema.Set
	withGrantOption bool
}

// currentGrantPolicyGrant is one row of live grant state discovered by
// readCurrentGrantPolicyGrants: one per real database/schema/table/
// sequence/function/procedure/foreign_data_wrapper/foreign_server object.
type currentGrantPolicyGrant struct {
	key             grantPolicyGrantKey
	object          string // "" for database/schema (the key's schema field already names them)
	privileges      *schema.Set
	withGrantOption bool
}

func resourcePostgreSQLGrantPolicy() *schema.Resource {
	return &schema.Resource{
		Create: PGResourceFunc(resourcePostgreSQLGrantPolicyCreateOrUpdate),
		Update: PGResourceFunc(resourcePostgreSQLGrantPolicyCreateOrUpdate),
		Read:   PGResourceFunc(resourcePostgreSQLGrantPolicyRead),
		Delete: PGResourceFunc(resourcePostgreSQLGrantPolicyDelete),
		Importer: &schema.ResourceImporter{
			StateContext: resourcePostgreSQLGrantPolicyImport,
		},

		Schema: map[string]*schema.Schema{
			"database": {
				Type:        schema.TypeString,
				Required:    true,
				ForceNew:    true,
				Description: "The database to manage grants for this role",
			},
			"role": {
				Type:        schema.TypeString,
				Required:    true,
				ForceNew:    true,
				Description: "The role to grant privileges to",
			},
			"authoritative": {
				Type:     schema.TypeBool,
				Optional: true,
				Default:  false,
				Description: "If true, this resource takes full ownership of every grant held by `role` in " +
					"`database` for the object types it manages (" + strings.Join(grantPolicyObjectTypes, ", ") +
					"): anything found in Postgres that is not declared in `grants` is REVOKED on every apply " +
					"and on destroy. If false (default), the resource only ensures the grants declared in " +
					"`grants` are present (additive mode) and never revokes privileges it did not declare.",
			},
			"grants": {
				Type:        schema.TypeList,
				Required:    true,
				Description: "The list of grants to apply for this (database, role) policy",
				// See resourcePostgreSQLDefaultPrivilegesPolicy's identical field for why this is
				// required: without it, this renders only as a repeated `grants { ... }` block,
				// not `grants = [ { ... } ]` attribute syntax.
				ConfigMode: schema.SchemaConfigModeAttr,
				Elem: &schema.Resource{
					Schema: map[string]*schema.Schema{
						"schema": {
							Type:     schema.TypeString,
							Optional: true,
							Description: "For object_type = \"schema\", the schema being granted on. For " +
								"table/sequence/function/procedure, the schema containing the objects. Must be " +
								"empty for database/foreign_data_wrapper/foreign_server.",
						},
						"object_type": {
							Type:         schema.TypeString,
							Required:     true,
							ValidateFunc: validation.StringInSlice(grantPolicyObjectTypes, false),
							Description:  "The PostgreSQL object type to grant privileges on (one of: " + strings.Join(grantPolicyObjectTypes, ", ") + ")",
						},
						"objects": {
							Type:        schema.TypeSet,
							Optional:    true,
							Elem:        &schema.Schema{Type: schema.TypeString},
							Set:         schema.HashString,
							Description: "The specific objects to grant privileges on. Required for foreign_data_wrapper/foreign_server. Disallowed for database/schema. Omitted/empty for table/sequence/function/procedure means every object of that type in the schema.",
						},
						"privileges": {
							Type:        schema.TypeSet,
							Required:    true,
							Elem:        &schema.Schema{Type: schema.TypeString},
							Set:         schema.HashString,
							Description: "The list of privileges to grant",
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
				Description: "Internal bookkeeping: exactly the grants this resource itself applied on its " +
					"most recent Create/Update, nothing else. Read and Import never write this field, " +
					"specifically so that grants merely observed in Postgres (e.g. a grant made by an " +
					"external SQL script for a (schema, object_type) never declared in `grants`) can never " +
					"end up here. When an entry is removed from `grants`, this is what `authoritative = " +
					"false` diffs against to decide what to revoke: only things this resource actually " +
					"granted before, never anything it only ever observed.",
				Elem: &schema.Resource{
					Schema: map[string]*schema.Schema{
						"schema":            {Type: schema.TypeString, Computed: true},
						"object_type":       {Type: schema.TypeString, Computed: true},
						"objects":           {Type: schema.TypeSet, Computed: true, Elem: &schema.Schema{Type: schema.TypeString}, Set: schema.HashString},
						"privileges":        {Type: schema.TypeSet, Computed: true, Elem: &schema.Schema{Type: schema.TypeString}, Set: schema.HashString},
						"with_grant_option": {Type: schema.TypeBool, Computed: true},
					},
				},
			},
		},
	}
}

func resourcePostgreSQLGrantPolicyRead(db *DBConnection, d *schema.ResourceData) error {
	if !db.featureSupported(featurePrivileges) {
		return fmt.Errorf("postgresql_grant_policy resource is not supported for this Postgres version (%s)", db.version)
	}

	database := d.Get("database").(string)
	role := d.Get("role").(string)

	exists, err := dbExists(db, database)
	if err != nil {
		return err
	}
	if !exists {
		d.SetId("")
		return nil
	}

	txn, err := startTransaction(db.client, database)
	if err != nil {
		return err
	}
	defer deferredRollback(txn)

	if role != publicRole {
		exists, err = roleExists(txn, role)
		if err != nil {
			return err
		}
		if !exists {
			d.SetId("")
			return nil
		}
	}

	return readGrantPolicy(txn, db, d)
}

func resourcePostgreSQLGrantPolicyCreateOrUpdate(db *DBConnection, d *schema.ResourceData) error {
	if !db.featureSupported(featurePrivileges) {
		return fmt.Errorf("postgresql_grant_policy resource is not supported for this Postgres version (%s)", db.version)
	}

	database := d.Get("database").(string)
	role := d.Get("role").(string)
	authoritative := d.Get("authoritative").(bool)

	grants, err := expandGrantPolicyGrants(d)
	if err != nil {
		return err
	}
	if err := validateGrantPolicyGrants(db, role, grants); err != nil {
		return err
	}

	// Must read before any d.Set call in this apply touches it.
	previouslyManaged := expandManagedGrantPolicyGrants(d)

	txn, err := startTransaction(db.client, database)
	if err != nil {
		return err
	}
	defer deferredRollback(txn)

	if err := pgLockRole(txn, role); err != nil {
		return err
	}

	if grantPolicyNeedsDatabaseLock(grants, authoritative) {
		if err := pgLockDatabase(txn, database); err != nil {
			return err
		}
	}

	roleOID, err := getRoleOID(txn, role)
	if err != nil {
		return err
	}

	owners, err := getRolesToGrantForPolicy(txn, grants)
	if err != nil {
		return err
	}

	if err := withRolesGranted(txn, owners, func() error {
		if authoritative {
			current, err := readCurrentGrantPolicyGrants(txn, db, database, roleOID)
			if err != nil {
				return err
			}
			for _, cur := range current {
				entry, declared := grants[cur.key]
				if declared && grantPolicyEntryCoversObject(entry, cur.object) {
					continue
				}
				if err := grantPolicyExec(txn, grantPolicyRevokeSQL(database, role, cur.key, singletonSet(cur.object), setStrings(cur.privileges))); err != nil {
					return fmt.Errorf("could not revoke undeclared grant: %w", err)
				}
			}
		} else {
			// Explicitly revoke entries this resource itself granted before that
			// are no longer declared. previouslyManaged only ever contains
			// entries this resource applied via a prior Create/Update (never
			// anything merely observed by Read or Import), so this can't touch
			// unmanaged drift like a grant made by an external SQL script.
			for key, value := range previouslyManaged {
				if _, declared := grants[key]; declared {
					continue
				}
				if err := grantPolicyExec(txn, grantPolicyRevokeSQL(database, role, key, value.objects, nil)); err != nil {
					return fmt.Errorf("could not revoke removed grant: %w", err)
				}
			}
		}

		// Sort for deterministic statement ordering (helps reading logs/errors).
		keys := make([]grantPolicyGrantKey, 0, len(grants))
		for key := range grants {
			keys = append(keys, key)
		}
		sort.Slice(keys, func(i, j int) bool { return grantPolicyKeyLess(keys[i], keys[j]) })

		for _, key := range keys {
			value := grants[key]
			// Revoke all privileges on the declared scope before granting,
			// otherwise reducing privileges will not work. This happens in the
			// same transaction so the role never loses privileges in between.
			if err := grantPolicyExec(txn, grantPolicyRevokeSQL(database, role, key, value.objects, nil)); err != nil {
				return fmt.Errorf("could not revoke prior privileges: %w", err)
			}
			if err := grantPolicyExec(txn, grantPolicyGrantSQL(database, role, key, value.objects, setStrings(value.privileges), value.withGrantOption)); err != nil {
				return fmt.Errorf("could not grant privileges: %w", err)
			}
		}

		return nil
	}); err != nil {
		return err
	}

	if err := txn.Commit(); err != nil {
		return fmt.Errorf("could not commit transaction: %w", err)
	}

	d.SetId(generateGrantPolicyID(database, role))

	// Record exactly what was just applied as the new ledger, before Read runs.
	if err := setManagedGrantPolicyGrants(d, grants); err != nil {
		return err
	}

	readTxn, err := startTransaction(db.client, database)
	if err != nil {
		return err
	}
	defer deferredRollback(readTxn)

	return readGrantPolicy(readTxn, db, d)
}

func resourcePostgreSQLGrantPolicyDelete(db *DBConnection, d *schema.ResourceData) error {
	database := d.Get("database").(string)
	role := d.Get("role").(string)
	authoritative := d.Get("authoritative").(bool)

	txn, err := startTransaction(db.client, database)
	if err != nil {
		return err
	}
	defer deferredRollback(txn)

	if err := pgLockRole(txn, role); err != nil {
		return err
	}

	managed := expandManagedGrantPolicyGrants(d)

	if grantPolicyNeedsDatabaseLock(managed, authoritative) {
		if err := pgLockDatabase(txn, database); err != nil {
			return err
		}
	}

	roleOID, err := getRoleOID(txn, role)
	if err != nil {
		return err
	}

	owners, err := getRolesToGrantForPolicy(txn, managed)
	if err != nil {
		return err
	}

	if err := withRolesGranted(txn, owners, func() error {
		if authoritative {
			// The policy owns the whole set of grants for this role: clean up
			// everything found, not just what is declared/managed.
			current, err := readCurrentGrantPolicyGrants(txn, db, database, roleOID)
			if err != nil {
				return err
			}
			for _, cur := range current {
				if err := grantPolicyExec(txn, grantPolicyRevokeSQL(database, role, cur.key, singletonSet(cur.object), setStrings(cur.privileges))); err != nil {
					return fmt.Errorf("could not revoke grant: %w", err)
				}
			}
			return nil
		}

		// Additive mode: only revoke what this resource actually granted — the
		// same managed_grants ledger Create/Update maintains, never anything
		// merely observed, so this stays correct even if a prior apply
		// partially failed.
		for key, value := range managed {
			if err := grantPolicyExec(txn, grantPolicyRevokeSQL(database, role, key, value.objects, nil)); err != nil {
				return fmt.Errorf("could not revoke grant: %w", err)
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

// resourcePostgreSQLGrantPolicyImport parses an import ID formatted as
// "<database>/<role>", discovers every grant currently held by that role in
// that database for the object types this resource manages, and seeds
// `grants` with the full result: on import there is no prior declared list
// to diff against, so everything found becomes part of the policy.
// `authoritative` is always forced to false (the safe default), regardless
// of what is live, and `managed_grants` is left empty — Import never writes
// the ledger, matching postgresql_default_privileges_policy's own import.
func resourcePostgreSQLGrantPolicyImport(ctx context.Context, d *schema.ResourceData, meta any) ([]*schema.ResourceData, error) {
	id := d.Id()
	parts := strings.SplitN(id, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return nil, fmt.Errorf("invalid ID %q for postgresql_grant_policy: expected format '<database>/<role>'", id)
	}
	database, role := parts[0], parts[1]

	client := meta.(*Client)
	db, err := client.Connect()
	if err != nil {
		return nil, err
	}

	if !db.featureSupported(featurePrivileges) {
		return nil, fmt.Errorf("postgresql_grant_policy resource is not supported for this Postgres version (%s)", db.version)
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

	if role != publicRole {
		exists, err = roleExists(txn, role)
		if err != nil {
			return nil, err
		}
		if !exists {
			return nil, fmt.Errorf("role %q does not exist", role)
		}
	}

	roleOID, err := getRoleOID(txn, role)
	if err != nil {
		return nil, err
	}

	current, err := readCurrentGrantPolicyGrants(txn, db, database, roleOID)
	if err != nil {
		return nil, err
	}

	if err := d.Set("database", database); err != nil {
		return nil, err
	}
	if err := d.Set("role", role); err != nil {
		return nil, err
	}
	if err := d.Set("authoritative", false); err != nil {
		return nil, err
	}
	if err := d.Set("grants", groupCurrentGrantPolicyGrantsForImport(current)); err != nil {
		return nil, err
	}

	d.SetId(generateGrantPolicyID(database, role))

	return []*schema.ResourceData{d}, nil
}

// expandGrantPolicyGrants parses and validates the `grants` block from the
// resource config into a map keyed by (schema, object_type).
func expandGrantPolicyGrants(d *schema.ResourceData) (map[grantPolicyGrantKey]grantPolicyGrantValue, error) {
	raw := d.Get("grants").([]interface{})
	grants := make(map[grantPolicyGrantKey]grantPolicyGrantValue, len(raw))

	for _, item := range raw {
		grant := item.(map[string]interface{})

		key := grantPolicyGrantKey{
			schema:     grant["schema"].(string),
			objectType: grant["object_type"].(string),
		}

		if _, exists := grants[key]; exists {
			return nil, fmt.Errorf(
				"duplicate grant entry for object_type %q, schema %q: each (schema, object_type) combination must appear only once in `grants`",
				key.objectType, key.schema,
			)
		}

		objects := grant["objects"].(*schema.Set)

		if key.schema == "" && sliceContainsStr(grantPolicySchemaScopedObjectTypes, key.objectType) {
			return nil, fmt.Errorf("`schema` is required when `object_type` is %q", key.objectType)
		}
		if key.schema != "" && sliceContainsStr(grantPolicyNoSchemaObjectTypes, key.objectType) {
			return nil, fmt.Errorf("`schema` must be empty when `object_type` is %q", key.objectType)
		}
		if objects.Len() > 0 && sliceContainsStr([]string{"database", "schema"}, key.objectType) {
			return nil, fmt.Errorf("cannot specify `objects` when `object_type` is `database` or `schema`")
		}
		if objects.Len() == 0 && sliceContainsStr(grantPolicyExplicitObjectTypes, key.objectType) {
			return nil, fmt.Errorf("`objects` is required when `object_type` is %q", key.objectType)
		}

		grants[key] = grantPolicyGrantValue{
			objects:         objects,
			privileges:      grant["privileges"].(*schema.Set),
			withGrantOption: grant["with_grant_option"].(bool),
		}
	}

	return grants, nil
}

// validateGrantPolicyGrants checks version support, `with_grant_option`
// against role "public", and allowed privileges for every declared grant.
func validateGrantPolicyGrants(db *DBConnection, role string, grants map[grantPolicyGrantKey]grantPolicyGrantValue) error {
	isPublic := strings.EqualFold(role, publicRole)

	for key, value := range grants {
		if isPublic && value.withGrantOption {
			return fmt.Errorf("with_grant_option cannot be true for role 'public'")
		}
		if key.objectType == "procedure" && !db.featureSupported(featureProcedure) {
			return fmt.Errorf("object type PROCEDURE is not supported for this Postgres version (%s)", db.version)
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

// expandManagedGrantPolicyGrants parses the managed_grants ledger. Only
// Create/Update ever write this field (see setManagedGrantPolicyGrants), so
// unlike `grants`, it can never contain an entry that came from an
// Import/Read snapshot of live Postgres state this resource didn't itself
// grant.
func expandManagedGrantPolicyGrants(d *schema.ResourceData) map[grantPolicyGrantKey]grantPolicyGrantValue {
	raw := d.Get("managed_grants").([]interface{})
	managed := make(map[grantPolicyGrantKey]grantPolicyGrantValue, len(raw))
	for _, item := range raw {
		entry := item.(map[string]interface{})
		key := grantPolicyGrantKey{
			schema:     entry["schema"].(string),
			objectType: entry["object_type"].(string),
		}
		managed[key] = grantPolicyGrantValue{
			objects:         entry["objects"].(*schema.Set),
			privileges:      entry["privileges"].(*schema.Set),
			withGrantOption: entry["with_grant_option"].(bool),
		}
	}
	return managed
}

// setManagedGrantPolicyGrants overwrites the managed_grants ledger with
// exactly what was just applied. Called only from Create/Update, after a
// successful commit.
func setManagedGrantPolicyGrants(d *schema.ResourceData, grants map[grantPolicyGrantKey]grantPolicyGrantValue) error {
	managed := make([]interface{}, 0, len(grants))
	for key, value := range grants {
		managed = append(managed, map[string]interface{}{
			"schema":            key.schema,
			"object_type":       key.objectType,
			"objects":           value.objects,
			"privileges":        value.privileges,
			"with_grant_option": value.withGrantOption,
		})
	}
	if err := d.Set("managed_grants", managed); err != nil {
		return fmt.Errorf("could not set managed_grants: %w", err)
	}
	return nil
}

func grantPolicyKeyLess(a, b grantPolicyGrantKey) bool {
	if a.schema != b.schema {
		return a.schema < b.schema
	}
	return a.objectType < b.objectType
}

// grantPolicyNeedsDatabaseLock reports whether a transaction must take
// pgLockDatabase before reconciling the given grants: either a database
// object_type entry is declared/managed, or the policy is authoritative (in
// which case bulk discovery might find, and need to revoke, a database-level
// grant even though none is declared).
func grantPolicyNeedsDatabaseLock(grants map[grantPolicyGrantKey]grantPolicyGrantValue, authoritative bool) bool {
	if authoritative {
		return true
	}
	for key := range grants {
		if key.objectType == "database" {
			return true
		}
	}
	return false
}

// grantPolicyEntryCoversObject reports whether a declared grant entry's
// `objects` covers a specific object name discovered live: empty `objects`
// covers every object of that (schema, object_type), otherwise only objects
// explicitly listed are covered.
func grantPolicyEntryCoversObject(entry grantPolicyGrantValue, object string) bool {
	return entry.objects.Len() == 0 || entry.objects.Contains(object)
}

// getRolesToGrantForPolicy computes the union, across every declared/managed
// entry, of roles that must be temporarily granted to the connected user (if
// not a superuser) in order to alter privileges on schemas/tables/sequences/
// functions/procedures owned by another role — the same purpose
// postgresql_grant's getRolesToGrant serves for a single grant, generalized
// across every schema referenced anywhere in one policy.
func getRolesToGrantForPolicy(txn *sql.Tx, grants map[grantPolicyGrantKey]grantPolicyGrantValue) ([]string, error) {
	seen := map[string]bool{}
	var owners []string
	add := func(owner string) {
		if !seen[owner] {
			seen[owner] = true
			owners = append(owners, owner)
		}
	}

	schemas := map[string]bool{}
	for key := range grants {
		if key.objectType == "schema" {
			schemas[key.schema] = true
		} else if sliceContainsStr(grantPolicySchemaScopedObjectTypes, key.objectType) {
			schemas[key.schema] = true
		}
	}

	schemaNames := make([]string, 0, len(schemas))
	for schemaName := range schemas {
		schemaNames = append(schemaNames, schemaName)
	}
	sort.Strings(schemaNames)

	for _, schemaName := range schemaNames {
		tableOwners, err := getTablesOwner(txn, schemaName)
		if err != nil {
			return nil, err
		}
		for _, owner := range tableOwners {
			add(owner)
		}

		schemaOwner, err := getSchemaOwner(txn, schemaName)
		if err != nil {
			return nil, err
		}
		add(schemaOwner)
	}

	return resolveOwners(txn, owners)
}

func setStrings(s *schema.Set) []string {
	out := make([]string, 0, s.Len())
	for _, v := range s.List() {
		out = append(out, v.(string))
	}
	return out
}

func singletonSet(v string) *schema.Set {
	return schema.NewSet(schema.HashString, []interface{}{v})
}

// grantPolicyExec runs a GRANT/REVOKE statement, silently skipping empty
// queries (e.g. an entry with zero privileges to grant).
func grantPolicyExec(txn *sql.Tx, query string) error {
	if query == "" {
		return nil
	}
	_, err := txn.Exec(query)
	return err
}

func grantPolicyGrantSQL(database, role string, key grantPolicyGrantKey, objects *schema.Set, privileges []string, withGrantOption bool) string {
	if len(privileges) == 0 {
		return ""
	}

	privList := strings.Join(privileges, ",")
	var query string

	switch key.objectType {
	case "database":
		query = fmt.Sprintf(
			"GRANT %s ON DATABASE %s TO %s",
			privList, pq.QuoteIdentifier(database), pq.QuoteIdentifier(role),
		)
	case "schema":
		query = fmt.Sprintf(
			"GRANT %s ON SCHEMA %s TO %s",
			privList, pq.QuoteIdentifier(key.schema), pq.QuoteIdentifier(role),
		)
	case "foreign_data_wrapper":
		query = fmt.Sprintf(
			"GRANT %s ON FOREIGN DATA WRAPPER %s TO %s",
			privList, setToPgIdentListWithoutSchema(objects), pq.QuoteIdentifier(role),
		)
	case "foreign_server":
		query = fmt.Sprintf(
			"GRANT %s ON FOREIGN SERVER %s TO %s",
			privList, setToPgIdentListWithoutSchema(objects), pq.QuoteIdentifier(role),
		)
	case "table", "sequence", "function", "procedure":
		objType := strings.ToUpper(key.objectType)
		if objects.Len() > 0 {
			query = fmt.Sprintf(
				"GRANT %s ON %s %s TO %s",
				privList, objType, setToPgIdentList(key.schema, objects), pq.QuoteIdentifier(role),
			)
		} else {
			query = fmt.Sprintf(
				"GRANT %s ON ALL %sS IN SCHEMA %s TO %s",
				privList, objType, pq.QuoteIdentifier(key.schema), pq.QuoteIdentifier(role),
			)
		}
	}

	if withGrantOption {
		query += " WITH GRANT OPTION"
	}

	return query
}

// grantPolicyRevokeSQL builds a REVOKE statement. A nil/empty `privileges`
// means REVOKE ALL PRIVILEGES (used when clearing a declared entry's scope
// before granting, or when dropping a whole managed entry); a non-empty
// `privileges` revokes exactly those (used for authoritative cleanup of a
// single discovered object, to avoid disturbing privileges outside what was
// actually observed, e.g. column-level grants on the same table).
func grantPolicyRevokeSQL(database, role string, key grantPolicyGrantKey, objects *schema.Set, privileges []string) string {
	switch key.objectType {
	case "database":
		return fmt.Sprintf(
			"REVOKE ALL PRIVILEGES ON DATABASE %s FROM %s",
			pq.QuoteIdentifier(database), pq.QuoteIdentifier(role),
		)
	case "schema":
		return fmt.Sprintf(
			"REVOKE ALL PRIVILEGES ON SCHEMA %s FROM %s",
			pq.QuoteIdentifier(key.schema), pq.QuoteIdentifier(role),
		)
	case "foreign_data_wrapper":
		return fmt.Sprintf(
			"REVOKE ALL PRIVILEGES ON FOREIGN DATA WRAPPER %s FROM %s",
			setToPgIdentListWithoutSchema(objects), pq.QuoteIdentifier(role),
		)
	case "foreign_server":
		return fmt.Sprintf(
			"REVOKE ALL PRIVILEGES ON FOREIGN SERVER %s FROM %s",
			setToPgIdentListWithoutSchema(objects), pq.QuoteIdentifier(role),
		)
	case "table", "sequence", "function", "procedure":
		objType := strings.ToUpper(key.objectType)
		if objects.Len() > 0 {
			if len(privileges) > 0 {
				return fmt.Sprintf(
					"REVOKE %s ON %s %s FROM %s",
					strings.Join(privileges, ","), objType, setToPgIdentList(key.schema, objects), pq.QuoteIdentifier(role),
				)
			}
			return fmt.Sprintf(
				"REVOKE ALL PRIVILEGES ON %s %s FROM %s",
				objType, setToPgIdentList(key.schema, objects), pq.QuoteIdentifier(role),
			)
		}
		return fmt.Sprintf(
			"REVOKE ALL PRIVILEGES ON ALL %sS IN SCHEMA %s FROM %s",
			objType, pq.QuoteIdentifier(key.schema), pq.QuoteIdentifier(role),
		)
	}
	return ""
}

// readCurrentGrantPolicyGrants returns every live grant found for `role` in
// `database`, across every object type postgresql_grant_policy manages, as
// one row per real object. It is the grant analogue of
// readCurrentDefaultPrivilegesPolicyGrants: instead of reading the single
// already-unioned pg_default_acl catalog, it UNIONs the several ACL catalogs
// grants are actually recorded in.
func readCurrentGrantPolicyGrants(txn *sql.Tx, db *DBConnection, database string, roleOID uint32) ([]currentGrantPolicyGrant, error) {
	// "procedure" (and pg_proc.prokind) do not exist before Postgres 11;
	// before that version everything in pg_proc is a function.
	proKindExpr := "'function'::text"
	if db.featureSupported(featureProcedure) {
		proKindExpr = "CASE p.prokind WHEN 'p' THEN 'procedure' ELSE 'function' END"
	}

	query := fmt.Sprintf(`
SELECT nspname, object_type, object_name, array_agg(privilege_type) AS privileges, bool_and(grantable) AS with_grant_option
FROM (
	SELECT NULL::text AS nspname, 'database'::text AS object_type, datname AS object_name, (aclexplode(datacl)).*
	FROM pg_database WHERE datname = $2
	UNION ALL
	SELECT nspname, 'schema'::text, nspname, (aclexplode(nspacl)).*
	FROM pg_namespace
	UNION ALL
	SELECT n.nspname, 'table'::text, c.relname, (aclexplode(c.relacl)).*
	FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
	WHERE c.relkind = 'r'
	UNION ALL
	SELECT n.nspname, 'sequence'::text, c.relname, (aclexplode(c.relacl)).*
	FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
	WHERE c.relkind = 'S'
	UNION ALL
	SELECT n.nspname, %s, p.proname, (aclexplode(p.proacl)).*
	FROM pg_proc p JOIN pg_namespace n ON n.oid = p.pronamespace
	UNION ALL
	SELECT NULL::text, 'foreign_data_wrapper'::text, fdwname, (aclexplode(fdwacl)).*
	FROM pg_foreign_data_wrapper
	UNION ALL
	SELECT NULL::text, 'foreign_server'::text, srvname, (aclexplode(srvacl)).*
	FROM pg_foreign_server
) AS t (nspname, object_type, object_name, grantor, grantee, privilege_type, grantable)
WHERE grantee = $1
GROUP BY nspname, object_type, object_name
`, proKindExpr)

	rows, err := txn.Query(query, roleOID, database)
	if err != nil {
		return nil, fmt.Errorf("could not read grants for role: %w", err)
	}
	defer rows.Close()

	var grants []currentGrantPolicyGrant
	for rows.Next() {
		var nspname sql.NullString
		var objectType string
		var objectName sql.NullString
		var privileges pq.ByteaArray
		var withGrantOption bool

		if err := rows.Scan(&nspname, &objectType, &objectName, &privileges, &withGrantOption); err != nil {
			return nil, fmt.Errorf("could not scan grant row: %w", err)
		}

		grants = append(grants, currentGrantPolicyGrant{
			key:             grantPolicyGrantKey{schema: nspname.String, objectType: objectType},
			object:          objectName.String,
			privileges:      pgArrayToSet(privileges),
			withGrantOption: withGrantOption,
		})
	}

	return grants, rows.Err()
}

// aggregateGrantPolicyEntry aggregates every discovered row matching `key`
// (and, when declaredObjects is non-empty, matching one of those object
// names) into a single (privileges, with_grant_option) pair: the
// intersection of privileges and the logical AND of with_grant_option across
// every matched object. found is false when nothing live matches — e.g. an
// empty schema for a whole-schema entry, or a declared object that has since
// been dropped.
func aggregateGrantPolicyEntry(current []currentGrantPolicyGrant, key grantPolicyGrantKey, declaredObjects *schema.Set) (privileges *schema.Set, withGrantOption bool, found bool) {
	var matched []currentGrantPolicyGrant
	for _, cur := range current {
		if cur.key != key {
			continue
		}
		if declaredObjects.Len() > 0 && !declaredObjects.Contains(cur.object) {
			continue
		}
		matched = append(matched, cur)
	}

	if len(matched) == 0 {
		return schema.NewSet(schema.HashString, nil), false, false
	}

	privileges = matched[0].privileges
	withGrantOption = matched[0].withGrantOption
	for _, m := range matched[1:] {
		privileges = privileges.Intersection(m.privileges)
		withGrantOption = withGrantOption && m.withGrantOption
	}
	return privileges, withGrantOption, true
}

// readGrantPolicy refreshes the `grants` privileges/with_grant_option for
// every entry already declared in state, reflecting drift that happened
// outside of Terraform. Mirrors readDefaultPrivilegesPolicy: entries with no
// live match refresh to an empty privilege set rather than erroring, so a
// dropped schema/table/role shows up as a normal diff on the next plan
// instead of a failure.
func readGrantPolicy(txn *sql.Tx, db *DBConnection, d *schema.ResourceData) error {
	database := d.Get("database").(string)
	role := d.Get("role").(string)
	declaredRaw := d.Get("grants").([]interface{})

	roleOID, err := getRoleOID(txn, role)
	if err != nil {
		return err
	}

	current, err := readCurrentGrantPolicyGrants(txn, db, database, roleOID)
	if err != nil {
		return err
	}

	grants := make([]interface{}, 0, len(declaredRaw))
	for _, item := range declaredRaw {
		declared := item.(map[string]interface{})

		key := grantPolicyGrantKey{
			schema:     declared["schema"].(string),
			objectType: declared["object_type"].(string),
		}
		declaredObjects := declared["objects"].(*schema.Set)

		privileges, withGrantOption, found := aggregateGrantPolicyEntry(current, key, declaredObjects)
		if !found {
			privileges = schema.NewSet(schema.HashString, nil)
			withGrantOption = false
		}

		grants = append(grants, map[string]interface{}{
			"schema":            key.schema,
			"object_type":       key.objectType,
			"objects":           declaredObjects,
			"privileges":        privileges,
			"with_grant_option": withGrantOption,
		})
	}

	if err := d.Set("grants", grants); err != nil {
		return fmt.Errorf("could not set grants: %w", err)
	}

	d.SetId(generateGrantPolicyID(database, role))

	return nil
}

func generateGrantPolicyID(database, role string) string {
	return strings.Join([]string{database, role}, "_")
}

// grantPolicySignature identifies a distinct (privileges, with_grant_option)
// combination, used by groupCurrentGrantPolicyGrantsForImport to decide
// whether every object sharing a (schema, object_type) can collapse into one
// whole-schema entry or must be split into several explicit-object entries.
type grantPolicySignature struct {
	privileges      string // sorted privileges, comma-joined
	withGrantOption bool
}

func grantPolicySignatureOf(cur currentGrantPolicyGrant) grantPolicySignature {
	privs := setStrings(cur.privileges)
	sort.Strings(privs)
	return grantPolicySignature{privileges: strings.Join(privs, ","), withGrantOption: cur.withGrantOption}
}

// groupCurrentGrantPolicyGrantsForImport groups discovered grants into
// grants[] entries, one per (schema, object_type, privilege-signature)
// group: whenever every object sharing a key has the same signature, the
// resulting entry has empty `objects` (whole-schema/whole-database);
// otherwise each distinct signature becomes its own entry with `objects`
// listing exactly the objects sharing it. foreign_data_wrapper/
// foreign_server always get explicit `objects`, since postgresql_grant_policy
// requires at least one there regardless of signature grouping (there is no
// "ALL FOREIGN DATA WRAPPERS" grant form).
func groupCurrentGrantPolicyGrantsForImport(current []currentGrantPolicyGrant) []interface{} {
	type group struct {
		objects         []string
		privileges      *schema.Set
		withGrantOption bool
	}

	byKey := map[grantPolicyGrantKey]map[grantPolicySignature]*group{}
	sigOrder := map[grantPolicyGrantKey][]grantPolicySignature{}
	var keyOrder []grantPolicyGrantKey

	for _, cur := range current {
		sig := grantPolicySignatureOf(cur)
		if byKey[cur.key] == nil {
			byKey[cur.key] = map[grantPolicySignature]*group{}
			keyOrder = append(keyOrder, cur.key)
		}
		g, ok := byKey[cur.key][sig]
		if !ok {
			g = &group{privileges: cur.privileges, withGrantOption: cur.withGrantOption}
			byKey[cur.key][sig] = g
			sigOrder[cur.key] = append(sigOrder[cur.key], sig)
		}
		if cur.object != "" {
			g.objects = append(g.objects, cur.object)
		}
	}

	sort.Slice(keyOrder, func(i, j int) bool { return grantPolicyKeyLess(keyOrder[i], keyOrder[j]) })

	var grants []interface{}
	for _, key := range keyOrder {
		sigs := sigOrder[key]
		sort.Slice(sigs, func(i, j int) bool { return sigs[i].privileges < sigs[j].privileges })

		collapseToWholeScope := len(sigs) == 1 && !sliceContainsStr(grantPolicyExplicitObjectTypes, key.objectType)

		for _, sig := range sigs {
			g := byKey[key][sig]

			objects := []interface{}{}
			if !collapseToWholeScope {
				sort.Strings(g.objects)
				for _, o := range g.objects {
					objects = append(objects, o)
				}
			}

			grants = append(grants, map[string]interface{}{
				"schema":            key.schema,
				"object_type":       key.objectType,
				"objects":           schema.NewSet(schema.HashString, objects),
				"privileges":        g.privileges,
				"with_grant_option": g.withGrantOption,
			})
		}
	}

	return grants
}
