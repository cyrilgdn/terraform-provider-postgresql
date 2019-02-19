package postgresql

import (
	"fmt"
	"strings"

	"database/sql"

	"github.com/hashicorp/errwrap"
	"github.com/lib/pq"
)

// pqQuoteLiteral returns a string literal safe for inclusion in a PostgreSQL
// query as a parameter.  The resulting string still needs to be wrapped in
// single quotes in SQL (i.e. fmt.Sprintf(`'%s'`, pqQuoteLiteral("str"))).  See
// quote_literal_internal() in postgresql/backend/utils/adt/quote.c:77.
func pqQuoteLiteral(in string) string {
	in = strings.Replace(in, `\`, `\\`, -1)
	in = strings.Replace(in, `'`, `''`, -1)
	return in
}

func validateConnLimit(v interface{}, key string) (warnings []string, errors []error) {
	value := v.(int)
	if value < -1 {
		errors = append(errors, fmt.Errorf("%s can not be less than -1", key))
	}
	return
}

func isRoleMember(db *sql.DB, role, member string) (bool, error) {
	var _rez int
	err := db.QueryRow(
		"SELECT 1 FROM pg_auth_members WHERE pg_get_userbyid(roleid) = $1 AND pg_get_userbyid(member) = $2",
		role, member,
	).Scan(&_rez)

	switch {
	case err == sql.ErrNoRows:
		return false, nil
	case err != nil:
		return false, errwrap.Wrapf("could not real role membership: {{err}}", err)
	}

	return true, nil
}

// grantRoleMembership grants the role *role* to the user *member*.
// It returns false if the grant is not needed because the user is already
// a member of this role.
func grantRoleMembership(db *sql.DB, role, member string) (bool, error) {
	if member == role {
		return false, nil
	}

	isMember, err := isRoleMember(db, role, member)
	if err != nil {
		return false, err
	}

	if isMember {
		return false, nil
	}

	sql := fmt.Sprintf("GRANT %s TO %s", pq.QuoteIdentifier(role), pq.QuoteIdentifier(member))
	if _, err := db.Exec(sql); err != nil {
		return false, errwrap.Wrapf(fmt.Sprintf(
			"Error granting role %s to %s: {{err}}", role, member,
		), err)
	}
	return true, nil
}

func revokeRoleMembership(db *sql.DB, role, member string) error {
	if member == role {
		return nil
	}

	isMember, err := isRoleMember(db, role, member)
	if err != nil {
		return err
	}

	if isMember {
		sql := fmt.Sprintf("REVOKE %s FROM %s", pq.QuoteIdentifier(role), pq.QuoteIdentifier(member))
		if _, err := db.Exec(sql); err != nil {
			return errwrap.Wrapf(fmt.Sprintf(
				"Error revoking role %s from %s: {{err}}", role, member,
			), err)
		}
	}
	return nil
}
