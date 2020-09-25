package postgresql

import (
	"database/sql"
	"fmt"
	"log"
	"strings"

	"github.com/hashicorp/terraform-plugin-sdk/helper/schema"
	"github.com/lib/pq"
)

// QueryAble is a DB connection (sql.DB/Tx)
type QueryAble interface {
	Exec(query string, args ...interface{}) (sql.Result, error)
	Query(query string, args ...interface{}) (*sql.Rows, error)
	QueryRow(query string, args ...interface{}) *sql.Row
}

// pqQuoteLiteral returns a string literal safe for inclusion in a PostgreSQL
// query as a parameter.  The resulting string still needs to be wrapped in
// single quotes in SQL (i.e. fmt.Sprintf(`'%s'`, pqQuoteLiteral("str"))).  See
// quote_literal_internal() in postgresql/backend/utils/adt/quote.c:77.
func pqQuoteLiteral(in string) string {
	in = strings.Replace(in, `\`, `\\`, -1)
	in = strings.Replace(in, `'`, `''`, -1)
	return in
}

func isMemberOfRole(db QueryAble, role, member string) (bool, error) {
	var _rez int
	err := db.QueryRow(
		"SELECT 1 FROM pg_auth_members WHERE pg_get_userbyid(roleid) = $1 AND pg_get_userbyid(member) = $2",
		role, member,
	).Scan(&_rez)

	switch {
	case err == sql.ErrNoRows:
		return false, nil
	case err != nil:
		return false, fmt.Errorf("could not read role membership: %w", err)
	}

	return true, nil
}

// grantRoleMembership grants the role *role* to the user *member*.
// It returns false if the grant is not needed because the user is already
// a member of this role.
func grantRoleMembership(db QueryAble, role, member string) (bool, error) {
	if member == role {
		return false, nil
	}

	isMember, err := isMemberOfRole(db, role, member)
	if err != nil {
		return false, err
	}

	if isMember {
		log.Printf("grantRoleMembership: %s is already a member of %s, nothing to do", member, role)
		return false, nil
	}

	log.Printf("grantRoleMembership: granting %s to %s", role, member)

	sql := fmt.Sprintf("GRANT %s TO %s", pq.QuoteIdentifier(role), pq.QuoteIdentifier(member))
	if _, err := db.Exec(sql); err != nil {
		return false, fmt.Errorf("Error granting role %s to %s: %w", role, member, err)
	}
	return true, nil
}

// revokeRoleMembership revokes the role *role* from the user *member*.
// It returns false if the revoke is not needed because the user is not a member of this role.
func revokeRoleMembership(db QueryAble, role, member string) (bool, error) {
	if member == role {
		return false, nil
	}

	isMember, err := isMemberOfRole(db, role, member)
	if err != nil {
		return false, err
	}
	if !isMember {
		return false, nil
	}

	log.Printf("revokeRoleMembership: Revoke %s from %s", role, member)

	sql := fmt.Sprintf("REVOKE %s FROM %s", pq.QuoteIdentifier(role), pq.QuoteIdentifier(member))
	if _, err := db.Exec(sql); err != nil {
		return false, fmt.Errorf("Error revoking role %s from %s: %w", role, member, err)
	}
	return true, nil
}

// withRolesGranted temporarily grants, if needed, the roles specified to connected user
// (i.e.: the admin configure in the provider) and revoke them as soon as the
// callback func has finished.
func withRolesGranted(txn *sql.Tx, roles []string, fn func() error) error {
	// No roles asked, execute the function directly
	if len(roles) == 0 {
		return fn()
	}

	currentUser, err := getCurrentUser(txn)
	if err != nil {
		return err
	}

	superuser, err := isSuperuser(txn, currentUser)
	if err != nil {
		return err
	}
	if superuser {
		log.Printf("withRolesGranted: current user %s is superuser, no need to grant roles", currentUser)
		return fn()
	}

	var grantedRoles []string
	var revokedRoles []string

	for _, role := range roles {
		// We need to check if the role we want to grant is a superuser
		// in this case Postgres disallows to grant it to a current user which is not superuser.
		superuser, err := isSuperuser(txn, role)
		if err != nil {
			return err
		}
		if superuser {
			log.Printf("withRolesGranted: WARN role %s could not be granted to current user (%s) as it's a superuser", role, currentUser)
			continue
		}

		// We also need to check if the reverse relationship does not exist.
		// e.g.: We want to temporary `GRANT foo TO postgres` so `postgres` become a member of role `foo`
		// in order to manipulate its objects/privileges.
		// But PostgreSQL prevents `foo` to be a member of the role `postgres`,
		// and for `postgres` to be a member of the role `foo`, at the same time.
		// In this case we will temporary revoke this privilege.
		// So, the following queries will happen (in the same transaction):
		//  - REVOKE postgres FROM foo
		//  - GRANT foo TO postgres
		//
		//     Here we execute the wrapped function `fn`
		//
		//  - REVOKE foo FROM postgres
		//  - GRANT postgres TO foo

		// Check the opposite relation and revoke currentUser from role if needed
		revoked, err := revokeRoleMembership(txn, currentUser, role)
		if err != nil {
			return err
		}
		if revoked {
			revokedRoles = append(revokedRoles, role)
		}

		// Grant the role to currentUser if needed
		roleGranted, err := grantRoleMembership(txn, role, currentUser)
		if err != nil {
			return err
		}
		if roleGranted {
			grantedRoles = append(grantedRoles, role)
		}
	}

	// Execute the wrapped function
	if err := fn(); err != nil {
		return err
	}

	// Revoke the temporary granted roles.
	for _, role := range grantedRoles {
		if _, err := revokeRoleMembership(txn, role, currentUser); err != nil {
			return err
		}
	}

	// Grant back the temporary revoked role.
	for _, role := range revokedRoles {
		// check if the role has not been deleted by the wrapped function
		exists, err := roleExists(txn, role)
		if err != nil {
			return err
		}
		if !exists {
			continue
		}
		if _, err := grantRoleMembership(txn, currentUser, role); err != nil {
			return err
		}
	}

	return nil
}

func sliceContainsStr(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}

// allowedPrivileges is the list of privileges allowed per object types in Postgres.
// see: https://www.postgresql.org/docs/current/sql-grant.html
var allowedPrivileges = map[string][]string{
	"database": []string{"ALL", "CREATE", "CONNECT", "TEMPORARY", "TEMP"},
	"table":    []string{"ALL", "SELECT", "INSERT", "UPDATE", "DELETE", "TRUNCATE", "REFERENCES", "TRIGGER"},
	"sequence": []string{"ALL", "USAGE", "SELECT", "UPDATE"},
	"function": []string{"ALL", "EXECUTE"},
	"type":     []string{"ALL", "USAGE"},
}

// validatePrivileges checks that privileges to apply are allowed for this object type.
func validatePrivileges(d *schema.ResourceData) error {
	objectType := d.Get("object_type").(string)
	privileges := d.Get("privileges").(*schema.Set).List()

	// Verify fields that are mandatory for specific object types
	if objectType != "database" && d.Get("schema").(string) == "" {
		return fmt.Errorf("parameter 'schema' is mandatory for object_type %s", objectType)
	}

	allowed, ok := allowedPrivileges[objectType]
	if !ok {
		return fmt.Errorf("unknown object type %s", objectType)
	}

	for _, priv := range privileges {
		if !sliceContainsStr(allowed, priv.(string)) {
			return fmt.Errorf("%s is not an allowed privilege for object type %s", priv, objectType)
		}
	}
	return nil
}

func pgArrayToSet(arr pq.ByteaArray) *schema.Set {
	s := make([]interface{}, len(arr))
	for i, v := range arr {
		s[i] = string(v)
	}
	return schema.NewSet(schema.HashString, s)
}

// startTransaction starts a new DB transaction on the specified database.
// If the database is specified and different from the one configured in the provider,
// it will create a new connection pool if needed.
func startTransaction(client *Client, database string) (*sql.Tx, error) {
	if database != "" && database != client.databaseName {
		var err error
		client, err = client.config.NewClient(database)
		if err != nil {
			return nil, err
		}
	}
	db := client.DB()
	txn, err := db.Begin()
	if err != nil {
		return nil, fmt.Errorf("could not start transaction: %w", err)
	}

	return txn, nil
}

func dbExists(db QueryAble, dbname string) (bool, error) {
	err := db.QueryRow("SELECT datname FROM pg_database WHERE datname=$1", dbname).Scan(&dbname)
	switch {
	case err == sql.ErrNoRows:
		return false, nil
	case err != nil:
		return false, fmt.Errorf("could not check if database exists: %w", err)
	}

	return true, nil
}

func roleExists(txn *sql.Tx, rolname string) (bool, error) {
	err := txn.QueryRow("SELECT 1 FROM pg_roles WHERE rolname=$1", rolname).Scan(&rolname)
	switch {
	case err == sql.ErrNoRows:
		return false, nil
	case err != nil:
		return false, fmt.Errorf("could not check if role exists: %w", err)
	}

	return true, nil
}

func schemaExists(txn *sql.Tx, schemaname string) (bool, error) {
	err := txn.QueryRow("SELECT 1 FROM pg_namespace WHERE nspname=$1", schemaname).Scan(&schemaname)
	switch {
	case err == sql.ErrNoRows:
		return false, nil
	case err != nil:
		return false, fmt.Errorf("could not check if schema exists: %w", err)
	}

	return true, nil
}

func getCurrentUser(db QueryAble) (string, error) {
	var currentUser string
	err := db.QueryRow("SELECT CURRENT_USER").Scan(&currentUser)
	switch {
	case err == sql.ErrNoRows:
		return "", fmt.Errorf("SELECT CURRENT_USER returns now row, this is quite disturbing")
	case err != nil:
		return "", fmt.Errorf("error while looking for the current user: %w", err)
	}
	return currentUser, nil
}

// deferredRollback can be used to rollback a transaction in a defer.
// It will log an error if it fails
func deferredRollback(txn *sql.Tx) {
	err := txn.Rollback()
	switch {
	case err == sql.ErrTxDone:
		// transaction has already been committed or rolled back
		log.Printf("[DEBUG]: %v", err)
	case err != nil:
		log.Printf("[ERR] could not rollback transaction: %v", err)
	}
}

func getDatabase(d *schema.ResourceData, client *Client) string {
	database := client.databaseName

	if v, ok := d.GetOk(extDatabaseAttr); ok {
		database = v.(string)
	}

	return database
}

func getDatabaseOwner(db QueryAble, database string) (string, error) {
	query := `
SELECT rolname
  FROM pg_database
  JOIN pg_roles ON datdba = pg_roles.oid
  WHERE datname = $1
`
	var owner string

	err := db.QueryRow(query, database).Scan(&owner)
	switch {
	case err == sql.ErrNoRows:
		return "", fmt.Errorf("could not find database '%s' while looking for owner", database)
	case err != nil:
		return "", fmt.Errorf("error while looking for the owner of database '%s': %w", database, err)
	}
	return owner, nil
}

func getSchemaOwner(db QueryAble, schemaName string) (string, error) {
	query := `
SELECT rolname
  FROM pg_namespace
  JOIN pg_roles ON nspowner = pg_roles.oid
  WHERE nspname = $1
`
	var owner string

	err := db.QueryRow(query, schemaName).Scan(&owner)
	switch {
	case err == sql.ErrNoRows:
		return "", fmt.Errorf("could not find schema '%s' while looking for owner", schemaName)
	case err != nil:
		return "", fmt.Errorf("error while looking for the owner of schema '%s': %w", schemaName, err)
	}
	return owner, nil
}

// getTablesOwner retrieves all the owners for all the tables in the specified schema.
func getTablesOwner(db QueryAble, schemaName string) ([]string, error) {
	rows, err := db.Query(
		"SELECT DISTINCT tableowner FROM pg_tables WHERE schemaname = $1",
		schemaName,
	)
	if err != nil {
		return nil, fmt.Errorf("error while looking for owners of tables in schema '%s': %w", schemaName, err)
	}

	var owners []string
	for rows.Next() {
		var owner string
		if err := rows.Scan(&owner); err != nil {
			return nil, fmt.Errorf("could not scan tables owner: %w", err)
		}
		owners = append(owners, owner)
	}

	return owners, nil
}

func isSuperuser(db QueryAble, role string) (bool, error) {
	var superuser bool

	if err := db.QueryRow("SELECT rolsuper FROM pg_roles WHERE rolname = $1", role).Scan(&superuser); err != nil {
		return false, fmt.Errorf("could not check if role %s is superuser: %w", role, err)
	}

	return superuser, nil
}
