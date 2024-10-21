package postgresql

import (
	"crypto/md5"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/validation"
	"github.com/lib/pq"
)

const (
	roleBypassRLSAttr                       = "bypass_row_level_security"
	roleConnLimitAttr                       = "connection_limit"
	roleCreateDBAttr                        = "create_database"
	roleCreateRoleAttr                      = "create_role"
	roleEncryptedPassAttr                   = "encrypted_password"
	roleIdleInTransactionSessionTimeoutAttr = "idle_in_transaction_session_timeout"
	roleInheritAttr                         = "inherit"
	roleLoginAttr                           = "login"
	roleNameAttr                            = "name"
	rolePasswordAttr                        = "password"
	roleReplicationAttr                     = "replication"
	roleSkipDropRoleAttr                    = "skip_drop_role"
	roleSkipReassignOwnedAttr               = "skip_reassign_owned"
	roleSuperuserAttr                       = "superuser"
	roleValidUntilAttr                      = "valid_until"
	roleRolesAttr                           = "roles"
	roleSearchPathAttr                      = "search_path"
	roleStatementTimeoutAttr                = "statement_timeout"
	roleAssumeRoleAttr                      = "assume_role"
	roleParameterAttr                       = "parameter"
	roleParameterNameAttr                   = "name"
	roleParameterValueAttr                  = "value"
	roleParameterQuoteAttr                  = "quote"

	// Deprecated options
	roleDepEncryptedAttr = "encrypted"
)

// These parameters have discrete attributes, so they are not supported by the parameter block
var ignoredRoleConfigurationParameters = []string{
	roleSearchPathAttr,
	roleIdleInTransactionSessionTimeoutAttr,
	roleStatementTimeoutAttr,
	"role",
}

func resourcePostgreSQLRole() *schema.Resource {
	return &schema.Resource{
		Create: PGResourceFunc(resourcePostgreSQLRoleCreate),
		Read:   PGResourceFunc(resourcePostgreSQLRoleRead),
		Update: PGResourceFunc(resourcePostgreSQLRoleUpdate),
		Delete: PGResourceFunc(resourcePostgreSQLRoleDelete),
		Exists: PGResourceExistsFunc(resourcePostgreSQLRoleExists),
		Importer: &schema.ResourceImporter{
			StateContext: schema.ImportStatePassthroughContext,
		},

		Schema: map[string]*schema.Schema{
			roleNameAttr: {
				Type:        schema.TypeString,
				Required:    true,
				Description: "The name of the role",
			},
			rolePasswordAttr: {
				Type:        schema.TypeString,
				Optional:    true,
				Sensitive:   true,
				Description: "Sets the role's password",
			},
			roleDepEncryptedAttr: {
				Type:       schema.TypeString,
				Optional:   true,
				Deprecated: fmt.Sprintf("Rename PostgreSQL role resource attribute %q to %q", roleDepEncryptedAttr, roleEncryptedPassAttr),
			},
			roleRolesAttr: {
				Type:        schema.TypeSet,
				Optional:    true,
				Elem:        &schema.Schema{Type: schema.TypeString},
				Set:         schema.HashString,
				MinItems:    0,
				Description: "Role(s) to grant to this new role",
			},
			roleSearchPathAttr: {
				Type:        schema.TypeList,
				Optional:    true,
				Elem:        &schema.Schema{Type: schema.TypeString},
				MinItems:    0,
				Description: "Sets the role's search path",
			},
			roleEncryptedPassAttr: {
				Type:        schema.TypeBool,
				Optional:    true,
				Default:     true,
				Description: "Control whether the password is stored encrypted in the system catalogs",
			},
			roleValidUntilAttr: {
				Type:        schema.TypeString,
				Optional:    true,
				Default:     "infinity",
				Description: "Sets a date and time after which the role's password is no longer valid",
			},
			roleConnLimitAttr: {
				Type:         schema.TypeInt,
				Optional:     true,
				Default:      -1,
				Description:  "How many concurrent connections can be made with this role",
				ValidateFunc: validation.IntAtLeast(-1),
			},
			roleSuperuserAttr: {
				Type:        schema.TypeBool,
				Optional:    true,
				Default:     false,
				Description: `Determine whether the new role is a "superuser"`,
			},
			roleCreateDBAttr: {
				Type:        schema.TypeBool,
				Optional:    true,
				Default:     false,
				Description: "Define a role's ability to create databases",
			},
			roleCreateRoleAttr: {
				Type:        schema.TypeBool,
				Optional:    true,
				Default:     false,
				Description: "Determine whether this role will be permitted to create new roles",
			},
			roleIdleInTransactionSessionTimeoutAttr: {
				Type:         schema.TypeInt,
				Optional:     true,
				Description:  "Terminate any session with an open transaction that has been idle for longer than the specified duration in milliseconds",
				ValidateFunc: validation.IntAtLeast(0),
			},
			roleInheritAttr: {
				Type:        schema.TypeBool,
				Optional:    true,
				Default:     true,
				Description: `Determine whether a role "inherits" the privileges of roles it is a member of`,
			},
			roleLoginAttr: {
				Type:        schema.TypeBool,
				Optional:    true,
				Default:     false,
				Description: "Determine whether a role is allowed to log in",
			},
			roleReplicationAttr: {
				Type:        schema.TypeBool,
				Optional:    true,
				Default:     false,
				Description: "Determine whether a role is allowed to initiate streaming replication or put the system in and out of backup mode",
			},
			roleBypassRLSAttr: {
				Type:        schema.TypeBool,
				Optional:    true,
				Default:     false,
				Description: "Determine whether a role bypasses every row-level security (RLS) policy",
			},
			roleSkipDropRoleAttr: {
				Type:        schema.TypeBool,
				Optional:    true,
				Default:     false,
				Description: "Skip actually running the DROP ROLE command when removing a ROLE from PostgreSQL",
			},
			roleSkipReassignOwnedAttr: {
				Type:        schema.TypeBool,
				Optional:    true,
				Default:     false,
				Description: "Skip actually running the REASSIGN OWNED command when removing a role from PostgreSQL",
			},
			roleStatementTimeoutAttr: {
				Type:         schema.TypeInt,
				Optional:     true,
				Description:  "Abort any statement that takes more than the specified number of milliseconds",
				ValidateFunc: validation.IntAtLeast(0),
			},
			roleAssumeRoleAttr: {
				Type:        schema.TypeString,
				Optional:    true,
				Description: "Role to switch to at login",
			},
			roleParameterAttr: {
				Type:        schema.TypeSet,
				Optional:    true,
				Description: "Configuration parameters",
				Elem:        resourcePostgreSQLRoleConfigurationParameter(),
			},
		},
	}
}

func resourcePostgreSQLRoleConfigurationParameter() *schema.Resource {
	return &schema.Resource{
		Schema: map[string]*schema.Schema{
			roleParameterNameAttr: {
				Type:         schema.TypeString,
				Required:     true,
				Description:  "Name of the configuration parameter to set",
				ValidateFunc: validation.StringNotInSlice(ignoredRoleConfigurationParameters, true),
			},
			roleParameterValueAttr: {
				Type:        schema.TypeString,
				Required:    true,
				Description: "Value of the configuration parameter",
			},
			roleParameterQuoteAttr: {
				Type:        schema.TypeBool,
				Optional:    true,
				Default:     true,
				Description: "Quote the parameter value as a literal",
			},
		},
	}
}

func resourcePostgreSQLRoleCreate(db *DBConnection, d *schema.ResourceData) error {
	txn, err := startTransaction(db.client, "")
	if err != nil {
		return err
	}
	defer deferredRollback(txn)

	stringOpts := []struct {
		hclKey string
		sqlKey string
	}{
		{rolePasswordAttr, "PASSWORD"},
		{roleValidUntilAttr, "VALID UNTIL"},
	}
	intOpts := []struct {
		hclKey string
		sqlKey string
	}{
		{roleConnLimitAttr, "CONNECTION LIMIT"},
	}

	type boolOptType struct {
		hclKey        string
		sqlKeyEnable  string
		sqlKeyDisable string
	}
	boolOpts := []boolOptType{
		{roleSuperuserAttr, "SUPERUSER", "NOSUPERUSER"},
		{roleCreateDBAttr, "CREATEDB", "NOCREATEDB"},
		{roleCreateRoleAttr, "CREATEROLE", "NOCREATEROLE"},
		{roleInheritAttr, "INHERIT", "NOINHERIT"},
		{roleLoginAttr, "LOGIN", "NOLOGIN"},
		// roleEncryptedPassAttr is used only when rolePasswordAttr is set.
		// {roleEncryptedPassAttr, "ENCRYPTED", "UNENCRYPTED"},
	}

	if db.featureSupported(featureRLS) {
		boolOpts = append(boolOpts, boolOptType{roleBypassRLSAttr, "BYPASSRLS", "NOBYPASSRLS"})
	}

	if db.featureSupported(featureReplication) {
		boolOpts = append(boolOpts, boolOptType{roleReplicationAttr, "REPLICATION", "NOREPLICATION"})
	}

	createOpts := make([]string, 0, len(stringOpts)+len(intOpts)+len(boolOpts))

	for _, opt := range stringOpts {
		v, ok := d.GetOk(opt.hclKey)
		if !ok {
			continue
		}

		val := v.(string)
		if val != "" {
			switch {
			case opt.hclKey == rolePasswordAttr:
				if strings.ToUpper(v.(string)) == "NULL" {
					createOpts = append(createOpts, "PASSWORD NULL")
				} else {
					if d.Get(roleEncryptedPassAttr).(bool) {
						createOpts = append(createOpts, "ENCRYPTED")
					} else {
						createOpts = append(createOpts, "UNENCRYPTED")
					}
					createOpts = append(createOpts, fmt.Sprintf("%s '%s'", opt.sqlKey, pqQuoteLiteral(val)))
				}
			case opt.hclKey == roleValidUntilAttr:
				switch {
				case v.(string) == "", strings.ToLower(v.(string)) == "infinity":
					createOpts = append(createOpts, fmt.Sprintf("%s '%s'", opt.sqlKey, "infinity"))
				default:
					createOpts = append(createOpts, fmt.Sprintf("%s '%s'", opt.sqlKey, pqQuoteLiteral(val)))
				}
			default:
				createOpts = append(createOpts, fmt.Sprintf("%s %s", opt.sqlKey, pq.QuoteIdentifier(val)))
			}
		}
	}

	for _, opt := range intOpts {
		val := d.Get(opt.hclKey).(int)
		createOpts = append(createOpts, fmt.Sprintf("%s %d", opt.sqlKey, val))
	}

	for _, opt := range boolOpts {
		if opt.hclKey == roleEncryptedPassAttr {
			// This attribute is handled above in the stringOpts
			// loop.
			continue
		}
		val := d.Get(opt.hclKey).(bool)
		valStr := opt.sqlKeyDisable
		if val {
			valStr = opt.sqlKeyEnable
		}
		createOpts = append(createOpts, valStr)
	}

	roleName := d.Get(roleNameAttr).(string)
	createStr := strings.Join(createOpts, " ")
	if len(createOpts) > 0 {
		if db.featureSupported(featureCreateRoleWith) {
			createStr = " WITH " + createStr
		} else {
			// NOTE(seanc@): Work around ParAccel/AWS RedShift's ancient fork of PostgreSQL
			createStr = " " + createStr
		}
	}

	sql := fmt.Sprintf("CREATE ROLE %s%s", pq.QuoteIdentifier(roleName), createStr)
	if _, err := txn.Exec(sql); err != nil {
		return fmt.Errorf("error creating role %s: %w", roleName, err)
	}

	if err = grantRoles(txn, d); err != nil {
		return err
	}

	if err = alterSearchPath(txn, d); err != nil {
		return err
	}

	if err = setStatementTimeout(txn, d); err != nil {
		return err
	}

	if err = setIdleInTransactionSessionTimeout(txn, d); err != nil {
		return err
	}

	if err = setAssumeRole(txn, d); err != nil {
		return err
	}

	if err = setConfigurationParameters(txn, d); err != nil {
		return err
	}

	if err = txn.Commit(); err != nil {
		return fmt.Errorf("could not commit transaction: %w", err)
	}

	d.SetId(roleName)

	return resourcePostgreSQLRoleReadImpl(db, d)
}

func resourcePostgreSQLRoleDelete(db *DBConnection, d *schema.ResourceData) error {
	roleName := d.Get(roleNameAttr).(string)

	txn, err := startTransaction(db.client, "")
	if err != nil {
		return err
	}
	defer deferredRollback(txn)

	if err := pgLockRole(txn, roleName); err != nil {
		return err
	}

	if !d.Get(roleSkipReassignOwnedAttr).(bool) {
		if err := withRolesGranted(txn, []string{roleName}, func() error {
			currentUser := db.client.config.getDatabaseUsername()
			if _, err := txn.Exec(fmt.Sprintf("REASSIGN OWNED BY %s TO %s", pq.QuoteIdentifier(roleName), pq.QuoteIdentifier(currentUser))); err != nil {
				return fmt.Errorf("could not reassign owned by role %s to %s: %w", roleName, currentUser, err)
			}

			if _, err := txn.Exec(fmt.Sprintf("DROP OWNED BY %s", pq.QuoteIdentifier(roleName))); err != nil {
				return fmt.Errorf("could not drop owned by role %s: %w", roleName, err)
			}
			return nil
		}); err != nil {
			return err
		}
	}
	if !d.Get(roleSkipDropRoleAttr).(bool) {
		if _, err := txn.Exec(fmt.Sprintf("DROP ROLE %s", pq.QuoteIdentifier(roleName))); err != nil {
			return fmt.Errorf("could not delete role %s: %w", roleName, err)
		}
	}

	if err := txn.Commit(); err != nil {
		return fmt.Errorf("Error committing schema: %w", err)
	}

	d.SetId("")

	return nil
}

func resourcePostgreSQLRoleExists(db *DBConnection, d *schema.ResourceData) (bool, error) {
	var roleName string
	err := db.QueryRow("SELECT rolname FROM pg_catalog.pg_roles WHERE rolname=$1", d.Id()).Scan(&roleName)
	switch {
	case err == sql.ErrNoRows:
		return false, nil
	case err != nil:
		return false, err
	}

	return true, nil
}

func resourcePostgreSQLRoleRead(db *DBConnection, d *schema.ResourceData) error {
	return resourcePostgreSQLRoleReadImpl(db, d)
}

func resourcePostgreSQLRoleReadImpl(db *DBConnection, d *schema.ResourceData) error {
	var roleSuperuser, roleInherit, roleCreateRole, roleCreateDB, roleCanLogin, roleReplication, roleBypassRLS bool
	var roleConnLimit int
	var roleName, roleValidUntil string
	var roleRoles, roleConfig pq.ByteaArray

	roleID := d.Id()

	columns := []string{
		"rolname",
		"rolsuper",
		"rolinherit",
		"rolcreaterole",
		"rolcreatedb",
		"rolcanlogin",
		"rolconnlimit",
		`COALESCE(rolvaliduntil::TEXT, 'infinity')`,
		"rolconfig",
	}

	values := []interface{}{
		&roleRoles,
		&roleName,
		&roleSuperuser,
		&roleInherit,
		&roleCreateRole,
		&roleCreateDB,
		&roleCanLogin,
		&roleConnLimit,
		&roleValidUntil,
		&roleConfig,
	}

	if db.featureSupported(featureReplication) {
		columns = append(columns, "rolreplication")
		values = append(values, &roleReplication)
	}

	if db.featureSupported(featureRLS) {
		columns = append(columns, "rolbypassrls")
		values = append(values, &roleBypassRLS)
	}

	roleSQL := fmt.Sprintf(`SELECT ARRAY(
			SELECT pg_get_userbyid(roleid) FROM pg_catalog.pg_auth_members members WHERE member = pg_roles.oid
		), %s
		FROM pg_catalog.pg_roles WHERE rolname=$1`,
		// select columns
		strings.Join(columns, ", "),
	)
	err := db.QueryRow(roleSQL, roleID).Scan(values...)

	switch {
	case err == sql.ErrNoRows:
		log.Printf("[WARN] PostgreSQL ROLE (%s) not found", roleID)
		d.SetId("")
		return nil
	case err != nil:
		return fmt.Errorf("Error reading ROLE: %w", err)
	}

	d.Set(roleNameAttr, roleName)
	d.Set(roleConnLimitAttr, roleConnLimit)
	d.Set(roleCreateDBAttr, roleCreateDB)
	d.Set(roleCreateRoleAttr, roleCreateRole)
	d.Set(roleEncryptedPassAttr, true)
	d.Set(roleInheritAttr, roleInherit)
	d.Set(roleLoginAttr, roleCanLogin)
	d.Set(roleSkipDropRoleAttr, d.Get(roleSkipDropRoleAttr).(bool))
	d.Set(roleSkipReassignOwnedAttr, d.Get(roleSkipReassignOwnedAttr).(bool))
	d.Set(roleSuperuserAttr, roleSuperuser)
	d.Set(roleValidUntilAttr, roleValidUntil)
	d.Set(roleReplicationAttr, roleReplication)
	d.Set(roleBypassRLSAttr, roleBypassRLS)
	d.Set(roleRolesAttr, pgArrayToSet(roleRoles))
	d.Set(roleSearchPathAttr, readSearchPath(roleConfig))
	d.Set(roleAssumeRoleAttr, readAssumeRole(roleConfig))

	statementTimeout, err := readStatementTimeout(roleConfig)
	if err != nil {
		return err
	}

	d.Set(roleStatementTimeoutAttr, statementTimeout)

	idleInTransactionSessionTimeout, err := readIdleInTransactionSessionTimeout(roleConfig)
	if err != nil {
		return err
	}

	d.Set(roleIdleInTransactionSessionTimeoutAttr, idleInTransactionSessionTimeout)

	d.SetId(roleName)

	password, err := readRolePassword(db, d, roleCanLogin)
	if err != nil {
		return err
	}

	d.Set(rolePasswordAttr, password)

	d.Set(roleParameterAttr, readRoleParameters(roleConfig, d.Get(roleParameterAttr).(*schema.Set)))
	return nil
}

// readSearchPath searches for a search_path entry in the rolconfig array.
// In case no such value is present, it returns nil.
func readSearchPath(roleConfig pq.ByteaArray) []string {
	for _, v := range roleConfig {
		config := string(v)
		if strings.HasPrefix(config, roleSearchPathAttr) {
			var result = strings.Split(strings.TrimPrefix(config, roleSearchPathAttr+"="), ", ")
			for i := range result {
				result[i] = strings.Trim(result[i], `"`)
			}
			return result
		}
	}
	return nil
}

// readIdleInTransactionSessionTimeout searches for a idle_in_transaction_session_timeout entry in the rolconfig array.
// In case no such value is present, it returns nil.
func readIdleInTransactionSessionTimeout(roleConfig pq.ByteaArray) (int, error) {
	for _, v := range roleConfig {
		config := string(v)
		if strings.HasPrefix(config, roleIdleInTransactionSessionTimeoutAttr) {
			var result = strings.Split(strings.TrimPrefix(config, roleIdleInTransactionSessionTimeoutAttr+"="), ", ")
			res, err := strconv.Atoi(result[0])
			if err != nil {
				return -1, fmt.Errorf("Error reading statement_timeout: %w", err)
			}
			return res, nil
		}
	}
	return 0, nil
}

// readStatementTimeout searches for a statement_timeout entry in the rolconfig array.
// In case no such value is present, it returns nil.
func readStatementTimeout(roleConfig pq.ByteaArray) (int, error) {
	for _, v := range roleConfig {
		config := string(v)
		if strings.HasPrefix(config, roleStatementTimeoutAttr) {
			var result = strings.Split(strings.TrimPrefix(config, roleStatementTimeoutAttr+"="), ", ")
			res, err := strconv.Atoi(result[0])
			if err != nil {
				return -1, fmt.Errorf("Error reading statement_timeout: %w", err)
			}
			return res, nil
		}
	}
	return 0, nil
}

// readAssumeRole searches for a role entry in the rolconfig array.
// In case no such value is present, it returns empty string.
func readAssumeRole(roleConfig pq.ByteaArray) string {
	var res string
	var assumeRoleAttr = "role"
	for _, v := range roleConfig {
		config := string(v)
		if strings.HasPrefix(config, assumeRoleAttr) {
			res = strings.TrimPrefix(config, assumeRoleAttr+"=")
		}
	}
	return res
}

func readRoleParameters(roleConfig pq.ByteaArray, existingParams *schema.Set) *schema.Set {
	params := make([]interface{}, 0)
	for _, v := range roleConfig {
		tokens := strings.Split(string(v), "=")
		if !sliceContainsStr(ignoredRoleConfigurationParameters, tokens[0]) {
			quote := true
			for _, p := range existingParams.List() {
				existingParam := p.(map[string]interface{})
				if existingParam[roleParameterNameAttr].(string) == tokens[0] {
					quote = existingParam[roleParameterQuoteAttr].(bool)
				}
			}
			params = append(params, map[string]interface{}{
				roleParameterNameAttr:  tokens[0],
				roleParameterValueAttr: tokens[1],
				roleParameterQuoteAttr: quote,
			})
		}
	}
	return schema.NewSet(schema.HashResource(resourcePostgreSQLRoleConfigurationParameter()), params)
}

// readRolePassword reads password either from Postgres if admin user is a superuser
// or only from Terraform state.
func readRolePassword(db *DBConnection, d *schema.ResourceData, roleCanLogin bool) (string, error) {
	statePassword := d.Get(rolePasswordAttr).(string)

	// Role which cannot login does not have password in pg_shadow.
	// Also, if user specifies that admin is not a superuser we don't try to read pg_shadow
	// (only superuser can read pg_shadow)
	if !roleCanLogin || !db.client.config.Superuser {
		return statePassword, nil
	}

	// Otherwise we check if connected user is really a superuser
	// (in order to warn user instead of having a permission denied error)
	superuser, err := db.isSuperuser()
	if err != nil {
		return "", err
	}
	if !superuser {
		return "", fmt.Errorf(
			"could not read role password from Postgres as "+
				"connected user %s is not a SUPERUSER. "+
				"You can set `superuser = false` in the provider configuration "+
				"so it will not try to read the password from Postgres",
			db.client.config.getDatabaseUsername(),
		)
	}

	var rolePassword string
	err = db.QueryRow("SELECT COALESCE(passwd, '') FROM pg_catalog.pg_shadow AS s WHERE s.usename = $1", d.Id()).Scan(&rolePassword)
	switch {
	case err == sql.ErrNoRows:
		// They don't have a password
		return "", nil
	case err != nil:
		return "", fmt.Errorf("Error reading role: %w", err)
	}
	// If the password isn't already in md5 format, but hashing the input
	// matches the password in the database for the user, they are the same
	if statePassword != "" && !strings.HasPrefix(statePassword, "md5") && !strings.HasPrefix(statePassword, "SCRAM-SHA-256") {
		if strings.HasPrefix(rolePassword, "md5") {
			hasher := md5.New()
			if _, err := hasher.Write([]byte(statePassword + d.Id())); err != nil {
				return "", err
			}
			hashedPassword := "md5" + hex.EncodeToString(hasher.Sum(nil))

			if hashedPassword == rolePassword {
				// The passwords are actually the same
				// make Terraform think they are the same
				return statePassword, nil
			}
		}
		if strings.HasPrefix(rolePassword, "SCRAM-SHA-256") {
			return statePassword, nil
			// TODO : implement scram-sha-256 challenge request to the server
		}
	}
	return rolePassword, nil
}

func resourcePostgreSQLRoleUpdate(db *DBConnection, d *schema.ResourceData) error {
	txn, err := startTransaction(db.client, "")
	if err != nil {
		return err
	}
	defer deferredRollback(txn)

	oldName, _ := d.GetChange(roleNameAttr)
	if err := pgLockRole(txn, oldName.(string)); err != nil {
		return err
	}

	if err := setRoleName(txn, d); err != nil {
		return err
	}

	if err := setRolePassword(txn, d); err != nil {
		return err
	}

	if err := setRoleBypassRLS(db, txn, d); err != nil {
		return err
	}

	if err := setRoleConnLimit(txn, d); err != nil {
		return err
	}

	if err := setRoleCreateDB(txn, d); err != nil {
		return err
	}

	if err := setRoleCreateRole(txn, d); err != nil {
		return err
	}

	if err := setRoleInherit(txn, d); err != nil {
		return err
	}

	if err := setRoleLogin(txn, d); err != nil {
		return err
	}

	if err := setRoleReplication(txn, d); err != nil {
		return err
	}

	if err := setRoleSuperuser(txn, d); err != nil {
		return err
	}

	if err := setRoleValidUntil(txn, d); err != nil {
		return err
	}

	// applying roles: let's revoke all / grant the right ones
	if err = revokeRoles(txn, d); err != nil {
		return err
	}

	if err = grantRoles(txn, d); err != nil {
		return err
	}

	if err = alterSearchPath(txn, d); err != nil {
		return err
	}

	if err = setStatementTimeout(txn, d); err != nil {
		return err
	}

	if err = setIdleInTransactionSessionTimeout(txn, d); err != nil {
		return err
	}

	if err = setAssumeRole(txn, d); err != nil {
		return err
	}

	if err = setConfigurationParameters(txn, d); err != nil {
		return err
	}

	if err = txn.Commit(); err != nil {
		return fmt.Errorf("could not commit transaction: %w", err)
	}

	return resourcePostgreSQLRoleReadImpl(db, d)
}

func setRoleName(txn *sql.Tx, d *schema.ResourceData) error {
	if !d.HasChange(roleNameAttr) {
		return nil
	}

	oraw, nraw := d.GetChange(roleNameAttr)
	o := oraw.(string)
	n := nraw.(string)
	if n == "" {
		return errors.New("Error setting role name to an empty string")
	}

	sql := fmt.Sprintf("ALTER ROLE %s RENAME TO %s", pq.QuoteIdentifier(o), pq.QuoteIdentifier(n))
	if _, err := txn.Exec(sql); err != nil {
		return fmt.Errorf("Error updating role NAME: %w", err)
	}

	d.SetId(n)

	return nil
}

func setRolePassword(txn *sql.Tx, d *schema.ResourceData) error {
	// If role is renamed, password is reset (as the md5 sum is also base on the role name)
	// so we need to update it
	if !d.HasChange(rolePasswordAttr) && !d.HasChange(roleNameAttr) {
		return nil
	}

	roleName := d.Get(roleNameAttr).(string)
	password := d.Get(rolePasswordAttr).(string)

	sql := fmt.Sprintf("ALTER ROLE %s PASSWORD '%s'", pq.QuoteIdentifier(roleName), pqQuoteLiteral(password))
	if _, err := txn.Exec(sql); err != nil {
		return fmt.Errorf("Error updating role password: %w", err)
	}
	return nil
}

func setRoleBypassRLS(db *DBConnection, txn *sql.Tx, d *schema.ResourceData) error {
	if !d.HasChange(roleBypassRLSAttr) {
		return nil
	}

	if !db.featureSupported(featureRLS) {
		return fmt.Errorf("PostgreSQL client is talking with a server (%q) that does not support PostgreSQL Row-Level Security", db.version.String())
	}

	bypassRLS := d.Get(roleBypassRLSAttr).(bool)
	tok := "NOBYPASSRLS"
	if bypassRLS {
		tok = "BYPASSRLS"
	}
	roleName := d.Get(roleNameAttr).(string)
	sql := fmt.Sprintf("ALTER ROLE %s WITH %s", pq.QuoteIdentifier(roleName), tok)
	if _, err := txn.Exec(sql); err != nil {
		return fmt.Errorf("Error updating role BYPASSRLS: %w", err)
	}

	return nil
}

func setRoleConnLimit(txn *sql.Tx, d *schema.ResourceData) error {
	if !d.HasChange(roleConnLimitAttr) {
		return nil
	}

	connLimit := d.Get(roleConnLimitAttr).(int)
	roleName := d.Get(roleNameAttr).(string)
	sql := fmt.Sprintf("ALTER ROLE %s CONNECTION LIMIT %d", pq.QuoteIdentifier(roleName), connLimit)
	if _, err := txn.Exec(sql); err != nil {
		return fmt.Errorf("Error updating role CONNECTION LIMIT: %w", err)
	}

	return nil
}

func setRoleCreateDB(txn *sql.Tx, d *schema.ResourceData) error {
	if !d.HasChange(roleCreateDBAttr) {
		return nil
	}

	createDB := d.Get(roleCreateDBAttr).(bool)
	tok := "NOCREATEDB"
	if createDB {
		tok = "CREATEDB"
	}
	roleName := d.Get(roleNameAttr).(string)
	sql := fmt.Sprintf("ALTER ROLE %s WITH %s", pq.QuoteIdentifier(roleName), tok)
	if _, err := txn.Exec(sql); err != nil {
		return fmt.Errorf("Error updating role CREATEDB: %w", err)
	}

	return nil
}

func setRoleCreateRole(txn *sql.Tx, d *schema.ResourceData) error {
	if !d.HasChange(roleCreateRoleAttr) {
		return nil
	}

	createRole := d.Get(roleCreateRoleAttr).(bool)
	tok := "NOCREATEROLE"
	if createRole {
		tok = "CREATEROLE"
	}
	roleName := d.Get(roleNameAttr).(string)
	sql := fmt.Sprintf("ALTER ROLE %s WITH %s", pq.QuoteIdentifier(roleName), tok)
	if _, err := txn.Exec(sql); err != nil {
		return fmt.Errorf("Error updating role CREATEROLE: %w", err)
	}

	return nil
}

func setRoleInherit(txn *sql.Tx, d *schema.ResourceData) error {
	if !d.HasChange(roleInheritAttr) {
		return nil
	}

	inherit := d.Get(roleInheritAttr).(bool)
	tok := "NOINHERIT"
	if inherit {
		tok = "INHERIT"
	}
	roleName := d.Get(roleNameAttr).(string)
	sql := fmt.Sprintf("ALTER ROLE %s WITH %s", pq.QuoteIdentifier(roleName), tok)
	if _, err := txn.Exec(sql); err != nil {
		return fmt.Errorf("Error updating role INHERIT: %w", err)
	}

	return nil
}

func setRoleLogin(txn *sql.Tx, d *schema.ResourceData) error {
	if !d.HasChange(roleLoginAttr) {
		return nil
	}

	login := d.Get(roleLoginAttr).(bool)
	tok := "NOLOGIN"
	if login {
		tok = "LOGIN"
	}
	roleName := d.Get(roleNameAttr).(string)
	sql := fmt.Sprintf("ALTER ROLE %s WITH %s", pq.QuoteIdentifier(roleName), tok)
	if _, err := txn.Exec(sql); err != nil {
		return fmt.Errorf("Error updating role LOGIN: %w", err)
	}

	return nil
}

func setRoleReplication(txn *sql.Tx, d *schema.ResourceData) error {
	if !d.HasChange(roleReplicationAttr) {
		return nil
	}

	replication := d.Get(roleReplicationAttr).(bool)
	tok := "NOREPLICATION"
	if replication {
		tok = "REPLICATION"
	}
	roleName := d.Get(roleNameAttr).(string)
	sql := fmt.Sprintf("ALTER ROLE %s WITH %s", pq.QuoteIdentifier(roleName), tok)
	if _, err := txn.Exec(sql); err != nil {
		return fmt.Errorf("Error updating role REPLICATION: %w", err)
	}

	return nil
}

func setRoleSuperuser(txn *sql.Tx, d *schema.ResourceData) error {
	if !d.HasChange(roleSuperuserAttr) {
		return nil
	}

	superuser := d.Get(roleSuperuserAttr).(bool)
	tok := "NOSUPERUSER"
	if superuser {
		tok = "SUPERUSER"
	}
	roleName := d.Get(roleNameAttr).(string)
	sql := fmt.Sprintf("ALTER ROLE %s WITH %s", pq.QuoteIdentifier(roleName), tok)
	if _, err := txn.Exec(sql); err != nil {
		return fmt.Errorf("Error updating role SUPERUSER: %w", err)
	}

	return nil
}

func setRoleValidUntil(txn *sql.Tx, d *schema.ResourceData) error {
	if !d.HasChange(roleValidUntilAttr) {
		return nil
	}

	validUntil := d.Get(roleValidUntilAttr).(string)
	if validUntil == "" {
		return nil
	} else if strings.ToLower(validUntil) == "infinity" {
		validUntil = "infinity"
	}

	roleName := d.Get(roleNameAttr).(string)
	sql := fmt.Sprintf("ALTER ROLE %s VALID UNTIL '%s'", pq.QuoteIdentifier(roleName), pqQuoteLiteral(validUntil))
	if _, err := txn.Exec(sql); err != nil {
		return fmt.Errorf("Error updating role VALID UNTIL: %w", err)
	}

	return nil
}

func revokeRoles(txn *sql.Tx, d *schema.ResourceData) error {
	role := d.Get(roleNameAttr).(string)

	query := `SELECT pg_get_userbyid(roleid)
		FROM pg_catalog.pg_auth_members members
		JOIN pg_catalog.pg_roles ON members.member = pg_roles.oid
		WHERE rolname = $1`

	rows, err := txn.Query(query, role)
	if err != nil {
		return fmt.Errorf("could not get roles list for role %s: %w", role, err)
	}
	defer rows.Close()

	grantedRoles := []string{}
	for rows.Next() {
		var grantedRole string

		if err = rows.Scan(&grantedRole); err != nil {
			return fmt.Errorf("could not scan role name for role %s: %w", role, err)
		}
		// We cannot revoke directly here as it shares the same cursor (with Tx)
		// and rows.Next seems to retrieve result row by row.
		// see: https://github.com/lib/pq/issues/81
		grantedRoles = append(grantedRoles, grantedRole)
	}

	for _, grantedRole := range grantedRoles {
		query = fmt.Sprintf("REVOKE %s FROM %s", pq.QuoteIdentifier(grantedRole), pq.QuoteIdentifier(role))

		log.Printf("[DEBUG] revoking role %s from %s", grantedRole, role)
		if _, err := txn.Exec(query); err != nil {
			return fmt.Errorf("could not revoke role %s from %s: %w", string(grantedRole), role, err)
		}
	}

	return nil
}

func grantRoles(txn *sql.Tx, d *schema.ResourceData) error {
	role := d.Get(roleNameAttr).(string)

	for _, grantingRole := range d.Get("roles").(*schema.Set).List() {
		query := fmt.Sprintf(
			"GRANT %s TO %s", pq.QuoteIdentifier(grantingRole.(string)), pq.QuoteIdentifier(role),
		)
		if _, err := txn.Exec(query); err != nil {
			return fmt.Errorf("could not grant role %s to %s: %w", grantingRole, role, err)
		}
	}
	return nil
}

func setConfigurationParameters(txn *sql.Tx, d *schema.ResourceData) error {
	role := d.Get(roleNameAttr).(string)
	if d.HasChange(roleParameterAttr) {
		o, n := d.GetChange(roleParameterAttr)
		oldParams := o.(*schema.Set)
		newParams := n.(*schema.Set)
		for _, p := range oldParams.List() {
			if !newParams.Contains(p) {
				param := p.(map[string]interface{})
				query := fmt.Sprintf(
					"ALTER ROLE %s RESET %s",
					pq.QuoteIdentifier(role),
					pq.QuoteIdentifier(param[roleParameterNameAttr].(string)))
				log.Printf("[DEBUG] setConfigurationParameters: %s", query)
				if _, err := txn.Exec(query); err != nil {
					return err
				}
			}
		}
		for _, p := range newParams.List() {
			if !oldParams.Contains(p) {
				param := p.(map[string]interface{})
				value := param[roleParameterValueAttr].(string)
				if param[roleParameterQuoteAttr].(bool) {
					value = pq.QuoteLiteral(value)
				}
				query := fmt.Sprintf(
					"ALTER ROLE %s SET %s TO %s",
					pq.QuoteIdentifier(role),
					pq.QuoteIdentifier(param[roleParameterNameAttr].(string)),
					value)
				log.Printf("[DEBUG] setConfigurationParameters: %s", query)
				if _, err := txn.Exec(query); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func alterSearchPath(txn *sql.Tx, d *schema.ResourceData) error {
	role := d.Get(roleNameAttr).(string)
	searchPathInterface := d.Get(roleSearchPathAttr).([]interface{})

	var searchPathString []string
	if len(searchPathInterface) > 0 {
		searchPathString = make([]string, len(searchPathInterface))
		for i, searchPathPart := range searchPathInterface {
			if strings.Contains(searchPathPart.(string), ", ") {
				return fmt.Errorf("search_path cannot contain `, `: %v", searchPathPart)
			}
			searchPathString[i] = pq.QuoteIdentifier(searchPathPart.(string))
		}
	} else {
		searchPathString = []string{"DEFAULT"}
	}
	searchPath := strings.Join(searchPathString[:], ", ")

	query := fmt.Sprintf(
		"ALTER ROLE %s SET search_path TO %s", pq.QuoteIdentifier(role), searchPath,
	)
	if _, err := txn.Exec(query); err != nil {
		return fmt.Errorf("could not set search_path %s for %s: %w", searchPath, role, err)
	}
	return nil
}

func setStatementTimeout(txn *sql.Tx, d *schema.ResourceData) error {
	if !d.HasChange(roleStatementTimeoutAttr) {
		return nil
	}

	roleName := d.Get(roleNameAttr).(string)
	statementTimeout := d.Get(roleStatementTimeoutAttr).(int)
	if statementTimeout != 0 {
		sql := fmt.Sprintf(
			"ALTER ROLE %s SET statement_timeout TO %d", pq.QuoteIdentifier(roleName), statementTimeout,
		)
		if _, err := txn.Exec(sql); err != nil {
			return fmt.Errorf("could not set statement_timeout %d for %s: %w", statementTimeout, roleName, err)
		}
	} else {
		sql := fmt.Sprintf(
			"ALTER ROLE %s RESET statement_timeout", pq.QuoteIdentifier(roleName),
		)
		if _, err := txn.Exec(sql); err != nil {
			return fmt.Errorf("could not reset statement_timeout for %s: %w", roleName, err)
		}
	}
	return nil
}

func setIdleInTransactionSessionTimeout(txn *sql.Tx, d *schema.ResourceData) error {
	if !d.HasChange(roleIdleInTransactionSessionTimeoutAttr) {
		return nil
	}

	roleName := d.Get(roleNameAttr).(string)
	idleInTransactionSessionTimeout := d.Get(roleIdleInTransactionSessionTimeoutAttr).(int)
	if idleInTransactionSessionTimeout != 0 {
		sql := fmt.Sprintf(
			"ALTER ROLE %s SET idle_in_transaction_session_timeout TO %d", pq.QuoteIdentifier(roleName), idleInTransactionSessionTimeout,
		)
		if _, err := txn.Exec(sql); err != nil {
			return fmt.Errorf("could not set idle_in_transaction_session_timeout %d for %s: %w", idleInTransactionSessionTimeout, roleName, err)
		}
	} else {
		sql := fmt.Sprintf(
			"ALTER ROLE %s RESET idle_in_transaction_session_timeout", pq.QuoteIdentifier(roleName),
		)
		if _, err := txn.Exec(sql); err != nil {
			return fmt.Errorf("could not reset idle_in_transaction_session_timeout for %s: %w", roleName, err)
		}
	}
	return nil
}

func setAssumeRole(txn *sql.Tx, d *schema.ResourceData) error {
	if !d.HasChange(roleAssumeRoleAttr) {
		return nil
	}

	roleName := d.Get(roleNameAttr).(string)
	assumeRole := d.Get(roleAssumeRoleAttr).(string)
	if assumeRole != "" {
		sql := fmt.Sprintf(
			"ALTER ROLE %s SET ROLE TO %s", pq.QuoteIdentifier(roleName), pq.QuoteIdentifier(assumeRole),
		)
		if _, err := txn.Exec(sql); err != nil {
			return fmt.Errorf("could not set role %s for %s: %w", assumeRole, roleName, err)
		}
	} else {
		sql := fmt.Sprintf(
			"ALTER ROLE %s RESET ROLE", pq.QuoteIdentifier(roleName),
		)
		if _, err := txn.Exec(sql); err != nil {
			return fmt.Errorf("could not reset role for %s: %w", roleName, err)
		}
	}
	return nil
}
