package postgresql

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"strings"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/lib/pq"
)

// PostgreSQL stores all per-database settings for a given (role, database)
// pair in a single row of pg_db_role_setting (unique on (setdatabase, setrole)).
// Concurrent ALTER ROLE … IN DATABASE … SET statements that target the same
// pair race on this unique constraint, so we serialize them with an advisory
// lock keyed by (role, database).
const acquireRoleDatabaseSettingLockSQL = `SELECT pg_advisory_xact_lock(hashtext($1), hashtext($2))`

const (
	roleDatabaseSettingRoleAttr      = "role"
	roleDatabaseSettingDatabaseAttr  = "database"
	roleDatabaseSettingParameterAttr = "parameter"
	roleDatabaseSettingValueAttr     = "value"
)

func resourcePostgreSQLRoleDatabaseSetting() *schema.Resource {
	return &schema.Resource{
		Create: PGResourceFunc(resourcePostgreSQLRoleDatabaseSettingCreate),
		Read:   PGResourceFunc(resourcePostgreSQLRoleDatabaseSettingRead),
		Update: PGResourceFunc(resourcePostgreSQLRoleDatabaseSettingUpdate),
		Delete: PGResourceFunc(resourcePostgreSQLRoleDatabaseSettingDelete),
		Importer: &schema.ResourceImporter{
			StateContext: resourcePostgreSQLRoleDatabaseSettingImport,
		},

		Schema: map[string]*schema.Schema{
			roleDatabaseSettingRoleAttr: {
				Type:        schema.TypeString,
				Required:    true,
				ForceNew:    true,
				Description: "The role whose per-database setting is managed (ALTER ROLE <role>).",
			},
			roleDatabaseSettingDatabaseAttr: {
				Type:        schema.TypeString,
				Required:    true,
				ForceNew:    true,
				Description: "The database in which the setting applies (IN DATABASE <database>).",
			},
			roleDatabaseSettingParameterAttr: {
				Type:             schema.TypeString,
				Required:         true,
				ForceNew:         true,
				DiffSuppressFunc: func(k, old, new string, d *schema.ResourceData) bool { return strings.EqualFold(old, new) },
				Description:      "The configuration parameter to set (e.g. role, search_path, statement_timeout).",
			},
			roleDatabaseSettingValueAttr: {
				Type:             schema.TypeString,
				Required:         true,
				DiffSuppressFunc: suppressSettingValueDiff,
				Description:      "The value to assign to the parameter for this (role, database) pair.",
			},
		},
	}
}

func resourcePostgreSQLRoleDatabaseSettingCreate(db *DBConnection, d *schema.ResourceData) error {
	if err := applyRoleDatabaseSetting(db, d); err != nil {
		return err
	}
	d.SetId(generateRoleDatabaseSettingID(d))
	return resourcePostgreSQLRoleDatabaseSettingReadImpl(db, d)
}

func resourcePostgreSQLRoleDatabaseSettingUpdate(db *DBConnection, d *schema.ResourceData) error {
	if err := applyRoleDatabaseSetting(db, d); err != nil {
		return err
	}
	return resourcePostgreSQLRoleDatabaseSettingReadImpl(db, d)
}

func resourcePostgreSQLRoleDatabaseSettingRead(db *DBConnection, d *schema.ResourceData) error {
	return resourcePostgreSQLRoleDatabaseSettingReadImpl(db, d)
}

func resourcePostgreSQLRoleDatabaseSettingDelete(db *DBConnection, d *schema.ResourceData) error {
	role := d.Get(roleDatabaseSettingRoleAttr).(string)
	database := d.Get(roleDatabaseSettingDatabaseAttr).(string)
	parameter := d.Get(roleDatabaseSettingParameterAttr).(string)

	// If role or database has been dropped externally, treat the setting as
	// already gone — RESET would otherwise fail with "role/database does not exist".
	exists, err := roleAndDatabaseExist(db, role, database)
	if err != nil {
		return err
	}
	if !exists {
		log.Printf("[WARN] PostgreSQL role %q or database %q no longer exists; skipping RESET %s", role, database, parameter)
		d.SetId("")
		return nil
	}

	txn, err := startTransaction(db.client, "")
	if err != nil {
		return err
	}
	defer deferredRollback(txn)

	if _, err := txn.Exec(acquireRoleDatabaseSettingLockSQL, role, database); err != nil {
		return fmt.Errorf("could not acquire role-database-setting lock: %w", err)
	}

	stmt := fmt.Sprintf(
		"ALTER ROLE %s IN DATABASE %s RESET %s",
		pq.QuoteIdentifier(role),
		pq.QuoteIdentifier(database),
		pq.QuoteIdentifier(parameter),
	)
	if _, err := txn.Exec(stmt); err != nil {
		_ = txn.Rollback()
		if exists, checkErr := roleAndDatabaseExist(db, role, database); checkErr == nil && !exists {
			log.Printf("[WARN] PostgreSQL role %q or database %q no longer exists; ignoring RESET %s error: %s", role, database, parameter, err)
			d.SetId("")
			return nil
		}
		return fmt.Errorf("could not reset %s for role %q in database %q: %w", parameter, role, database, err)
	}
	if err := txn.Commit(); err != nil {
		return fmt.Errorf("could not commit role-database-setting reset: %w", err)
	}
	d.SetId("")
	return nil
}

func applyRoleDatabaseSetting(db *DBConnection, d *schema.ResourceData) error {
	role := d.Get(roleDatabaseSettingRoleAttr).(string)
	database := d.Get(roleDatabaseSettingDatabaseAttr).(string)
	parameter := d.Get(roleDatabaseSettingParameterAttr).(string)
	value := d.Get(roleDatabaseSettingValueAttr).(string)

	txn, err := startTransaction(db.client, "")
	if err != nil {
		return err
	}
	defer deferredRollback(txn)

	if _, err := txn.Exec(acquireRoleDatabaseSettingLockSQL, role, database); err != nil {
		return fmt.Errorf("could not acquire role-database-setting lock: %w", err)
	}

	var stmt string
	if isListParameter(parameter) {
		stmt = fmt.Sprintf(
			"ALTER ROLE %s IN DATABASE %s SET %s TO %s",
			pq.QuoteIdentifier(role),
			pq.QuoteIdentifier(database),
			pq.QuoteIdentifier(parameter),
			formatListSettingValue(value),
		)
	} else {
		stmt = fmt.Sprintf(
			"ALTER ROLE %s IN DATABASE %s SET %s = %s",
			pq.QuoteIdentifier(role),
			pq.QuoteIdentifier(database),
			pq.QuoteIdentifier(parameter),
			pq.QuoteLiteral(value),
		)
	}
	if _, err := txn.Exec(stmt); err != nil {
		return fmt.Errorf("could not set %s for role %q in database %q: %w", parameter, role, database, err)
	}
	if err := txn.Commit(); err != nil {
		return fmt.Errorf("could not commit role-database-setting update: %w", err)
	}
	return nil
}

func resourcePostgreSQLRoleDatabaseSettingReadImpl(db *DBConnection, d *schema.ResourceData) error {
	role := d.Get(roleDatabaseSettingRoleAttr).(string)
	database := d.Get(roleDatabaseSettingDatabaseAttr).(string)
	parameter := d.Get(roleDatabaseSettingParameterAttr).(string)

	const query = `
SELECT s.setconfig
FROM pg_db_role_setting s
JOIN pg_roles r ON r.oid = s.setrole
JOIN pg_database dbs ON dbs.oid = s.setdatabase
WHERE r.rolname = $1 AND dbs.datname = $2
`
	var setconfig []string
	err := db.QueryRow(query, role, database).Scan(pq.Array(&setconfig))
	switch {
	case err == sql.ErrNoRows:
		log.Printf("[WARN] no pg_db_role_setting row for role %q in database %q", role, database)
		d.SetId("")
		return nil
	case err != nil:
		return fmt.Errorf("error reading role database setting: %w", err)
	}

	value, found := findSetconfigValue(setconfig, parameter)
	if !found {
		log.Printf("[WARN] parameter %q not found in setconfig for role %q in database %q", parameter, role, database)
		d.SetId("")
		return nil
	}

	d.Set(roleDatabaseSettingRoleAttr, role)
	d.Set(roleDatabaseSettingDatabaseAttr, database)
	d.Set(roleDatabaseSettingParameterAttr, parameter)
	d.Set(roleDatabaseSettingValueAttr, value)
	d.SetId(generateRoleDatabaseSettingID(d))
	return nil
}

// findSetconfigValue searches a pg_db_role_setting.setconfig array (each
// element formatted as "key=value") for the requested parameter and returns
// the value verbatim. Parameter name comparison is case-insensitive because
// PostgreSQL canonicalizes GUC names to lowercase in the catalog.
func findSetconfigValue(setconfig []string, parameter string) (string, bool) {
	target := strings.ToLower(parameter)
	for _, entry := range setconfig {
		k, v, ok := strings.Cut(entry, "=")
		if !ok {
			continue
		}
		if strings.ToLower(k) == target {
			return v, true
		}
	}
	return "", false
}

func isListParameter(param string) bool {
	switch strings.ToLower(param) {
	case "search_path",
		"temp_tablespaces":
		return true
	default:
		return false
	}
}

// splitListSetting splits a comma-separated list of values, respecting
// double quotes and single quotes so commas inside identifiers (e.g. "a,b")
// are not treated as separators.
func splitListSetting(v string) []string {
	var items []string
	var cur strings.Builder
	inDouble := false
	inSingle := false

	for i := 0; i < len(v); i++ {
		c := v[i]
		switch {
		case c == '"' && !inSingle:
			inDouble = !inDouble
			cur.WriteByte(c)
		case c == '\'' && !inDouble:
			inSingle = !inSingle
			cur.WriteByte(c)
		case c == ',' && !inDouble && !inSingle:
			item := strings.TrimSpace(cur.String())
			if item != "" {
				items = append(items, item)
			}
			cur.Reset()
		default:
			cur.WriteByte(c)
		}
	}
	item := strings.TrimSpace(cur.String())
	if item != "" {
		items = append(items, item)
	}
	return items
}

// formatListSettingValue serializes a list of values for use in
// ALTER ROLE ... SET <param> TO <items>.
func formatListSettingValue(v string) string {
	items := splitListSetting(v)
	if len(items) == 0 {
		return "''"
	}
	quoted := make([]string, len(items))
	for i, item := range items {
		item = strings.TrimSpace(item)
		var inner string
		if len(item) >= 2 && item[0] == '"' && item[len(item)-1] == '"' {
			inner = strings.ReplaceAll(item[1:len(item)-1], `""`, `"`)
		} else if len(item) >= 2 && item[0] == '\'' && item[len(item)-1] == '\'' {
			inner = strings.ReplaceAll(item[1:len(item)-1], `''`, `'`)
		} else {
			inner = strings.ToLower(item)
		}
		if inner == "" {
			quoted[i] = "''"
		} else {
			quoted[i] = pq.QuoteIdentifier(inner)
		}
	}
	return strings.Join(quoted, ", ")
}

// canonicalIdentifier normalizes a list identifier according to PostgreSQL
// semantics: unquoted identifiers fold to lowercase, while quoted identifiers
// preserve their exact casing.
func canonicalIdentifier(item string) string {
	item = strings.TrimSpace(item)
	if len(item) >= 2 && item[0] == '"' && item[len(item)-1] == '"' {
		return strings.ReplaceAll(item[1:len(item)-1], `""`, `"`)
	}
	if len(item) >= 2 && item[0] == '\x27' && item[len(item)-1] == '\x27' {
		return strings.ReplaceAll(item[1:len(item)-1], `''`, `'`)
	}
	return strings.ToLower(item)
}

// suppressSettingValueDiff suppresses harmless differences in list-valued
// parameters caused by whitespace around commas, single vs double quotes,
// and empty lists, while respecting PostgreSQL case-folding rules.
func suppressSettingValueDiff(k, old, new string, d *schema.ResourceData) bool {
	if old == new {
		return true
	}
	parameter := d.Get(roleDatabaseSettingParameterAttr).(string)
	if isListParameter(parameter) {
		// PostgreSQL stores an empty search_path as `""` in pg_db_role_setting.setconfig
		if (old == `""` && new == "") || (old == "" && new == `""`) {
			return true
		}
		oldItems := splitListSetting(old)
		newItems := splitListSetting(new)
		if len(oldItems) != len(newItems) {
			return false
		}
		for i := range oldItems {
			if canonicalIdentifier(oldItems[i]) != canonicalIdentifier(newItems[i]) {
				return false
			}
		}
		return true
	}
	return false
}

func roleAndDatabaseExist(db *DBConnection, role, database string) (bool, error) {
	var ok bool
	err := db.QueryRow(
		`SELECT EXISTS(SELECT 1 FROM pg_roles WHERE rolname = $1)
		    AND EXISTS(SELECT 1 FROM pg_database WHERE datname = $2)`,
		role, database,
	).Scan(&ok)
	if err != nil {
		return false, fmt.Errorf("could not check existence of role %q / database %q: %w", role, database, err)
	}
	return ok, nil
}

// generateRoleDatabaseSettingID encodes the (role, database, parameter)
// triple into a Terraform resource ID. The components are joined with ':',
// with literal ':' and '\' inside any component backslash-escaped so the ID
// round-trips for any valid PostgreSQL identifier (which can contain ':'
// when quoted). Components without these characters round-trip without any
// visible escaping.
func generateRoleDatabaseSettingID(d *schema.ResourceData) string {
	return strings.Join([]string{
		escapeIDComponent(d.Get(roleDatabaseSettingRoleAttr).(string)),
		escapeIDComponent(d.Get(roleDatabaseSettingDatabaseAttr).(string)),
		escapeIDComponent(d.Get(roleDatabaseSettingParameterAttr).(string)),
	}, ":")
}

func escapeIDComponent(s string) string {
	// Order matters: escape backslashes first, then colons, so ':' inserted
	// here doesn't get re-escaped on the second pass.
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `:`, `\:`)
	return s
}

// splitIDComponents splits the encoded ID on un-escaped ':' separators and
// undoes the backslash escaping applied by escapeIDComponent. It returns
// however many components are present; callers validate the count.
func splitIDComponents(id string) ([]string, error) {
	var parts []string
	var cur strings.Builder
	for i := 0; i < len(id); i++ {
		if id[i] == '\\' {
			if i+1 >= len(id) {
				return nil, fmt.Errorf("trailing unescaped backslash in ID %q", id)
			}
			next := id[i+1]
			if next == ':' || next == '\\' {
				cur.WriteByte(next)
				i++
				continue
			}
			return nil, fmt.Errorf("invalid escape sequence \\%c in ID %q: only \\: and \\\\ are permitted", next, id)
		}
		if id[i] == ':' {
			parts = append(parts, cur.String())
			cur.Reset()
			continue
		}
		cur.WriteByte(id[i])
	}
	parts = append(parts, cur.String())
	return parts, nil
}

func resourcePostgreSQLRoleDatabaseSettingImport(ctx context.Context, d *schema.ResourceData, meta any) ([]*schema.ResourceData, error) {
	parts, err := splitIDComponents(d.Id())
	if err != nil {
		return nil, fmt.Errorf("invalid postgresql_role_database_setting import ID %q: %w", d.Id(), err)
	}
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
		return nil, fmt.Errorf(
			"invalid postgresql_role_database_setting import ID %q: expected <role>:<database>:<parameter>, with literal ':' and '\\' in any component backslash-escaped (use single quotes in the shell to preserve backslashes)",
			d.Id(),
		)
	}
	d.Set(roleDatabaseSettingRoleAttr, parts[0])
	d.Set(roleDatabaseSettingDatabaseAttr, parts[1])
	d.Set(roleDatabaseSettingParameterAttr, strings.ToLower(parts[2]))
	return []*schema.ResourceData{d}, nil
}
