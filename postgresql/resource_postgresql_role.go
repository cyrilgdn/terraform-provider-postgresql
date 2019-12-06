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

	"github.com/hashicorp/errwrap"
	"github.com/hashicorp/terraform-plugin-sdk/helper/schema"
	"github.com/lib/pq"
)

const (
	roleBypassRLSAttr         = "bypass_row_level_security"
	roleConnLimitAttr         = "connection_limit"
	roleCreateDBAttr          = "create_database"
	roleCreateRoleAttr        = "create_role"
	roleEncryptedPassAttr     = "encrypted_password"
	roleInheritAttr           = "inherit"
	roleLoginAttr             = "login"
	roleNameAttr              = "name"
	rolePasswordAttr          = "password"
	roleReplicationAttr       = "replication"
	roleSkipDropRoleAttr      = "skip_drop_role"
	roleSkipReassignOwnedAttr = "skip_reassign_owned"
	roleSuperuserAttr         = "superuser"
	roleValidUntilAttr        = "valid_until"
	roleRolesAttr             = "roles"
	roleSearchPathAttr        = "search_path"
	roleStatementTimeoutAttr  = "statement_timeout"

	// Deprecated options
	roleDepEncryptedAttr = "encrypted"
)

func resourcePostgreSQLRole() *schema.Resource {
	return &schema.Resource{
		Create: resourcePostgreSQLRoleCreate,
		Read:   resourcePostgreSQLRoleRead,
		Update: resourcePostgreSQLRoleUpdate,
		Delete: resourcePostgreSQLRoleDelete,
		Exists: resourcePostgreSQLRoleExists,
		Importer: &schema.ResourceImporter{
			State: schema.ImportStatePassthrough,
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
				ValidateFunc: validateConnLimit,
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
				ValidateFunc: validateStatementTimeout,
			},
		},
	}
}

func resourcePostgreSQLRoleCreate(d *schema.ResourceData, meta interface{}) error {
	c := meta.(*Client)
	c.catalogLock.Lock()
	defer c.catalogLock.Unlock()

	txn, err := c.DB().Begin()
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

	if c.featureSupported(featureRLS) {
		boolOpts = append(boolOpts, boolOptType{roleBypassRLSAttr, "BYPASSRLS", "NOBYPASSRLS"})
	}

	if c.featureSupported(featureReplication) {
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
		if c.featureSupported(featureCreateRoleWith) {
			createStr = " WITH " + createStr
		} else {
			// NOTE(seanc@): Work around ParAccel/AWS RedShift's ancient fork of PostgreSQL
			createStr = " " + createStr
		}
	}

	sql := fmt.Sprintf("CREATE ROLE %s%s", pq.QuoteIdentifier(roleName), createStr)
	if _, err := txn.Exec(sql); err != nil {
		return errwrap.Wrapf(fmt.Sprintf("error creating role %s: {{err}}", roleName), err)
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

	if err = txn.Commit(); err != nil {
		return errwrap.Wrapf("could not commit transaction: {{err}}", err)
	}

	d.SetId(roleName)

	return resourcePostgreSQLRoleReadImpl(c, d)
}

func resourcePostgreSQLRoleDelete(d *schema.ResourceData, meta interface{}) error {
	c := meta.(*Client)
	c.catalogLock.Lock()
	defer c.catalogLock.Unlock()

	txn, err := c.DB().Begin()
	if err != nil {
		return err
	}
	defer deferredRollback(txn)

	roleName := d.Get(roleNameAttr).(string)

	queries := make([]string, 0, 3)
	if !d.Get(roleSkipReassignOwnedAttr).(bool) {
		if c.featureSupported(featureReassignOwnedCurrentUser) {
			queries = append(queries, fmt.Sprintf("REASSIGN OWNED BY %s TO CURRENT_USER", pq.QuoteIdentifier(roleName)))
		} else {
			queries = append(queries, fmt.Sprintf("REASSIGN OWNED BY %s TO %s", pq.QuoteIdentifier(roleName), pq.QuoteIdentifier(c.config.getDatabaseUsername())))
		}
		queries = append(queries, fmt.Sprintf("DROP OWNED BY %s", pq.QuoteIdentifier(roleName)))
	}

	if !d.Get(roleSkipDropRoleAttr).(bool) {
		queries = append(queries, fmt.Sprintf("DROP ROLE %s", pq.QuoteIdentifier(roleName)))
	}

	if len(queries) > 0 {
		for _, query := range queries {
			if _, err := txn.Exec(query); err != nil {
				return errwrap.Wrapf("Error deleting role: {{err}}", err)
			}
		}

		if err := txn.Commit(); err != nil {
			return errwrap.Wrapf("Error committing schema: {{err}}", err)
		}
	}

	d.SetId("")

	return nil
}

func resourcePostgreSQLRoleExists(d *schema.ResourceData, meta interface{}) (bool, error) {
	c := meta.(*Client)
	c.catalogLock.RLock()
	defer c.catalogLock.RUnlock()

	var roleName string
	err := c.DB().QueryRow("SELECT rolname FROM pg_catalog.pg_roles WHERE rolname=$1", d.Id()).Scan(&roleName)
	switch {
	case err == sql.ErrNoRows:
		return false, nil
	case err != nil:
		return false, err
	}

	return true, nil
}

func resourcePostgreSQLRoleRead(d *schema.ResourceData, meta interface{}) error {
	c := meta.(*Client)
	c.catalogLock.RLock()
	defer c.catalogLock.RUnlock()

	return resourcePostgreSQLRoleReadImpl(c, d)
}

func resourcePostgreSQLRoleReadImpl(c *Client, d *schema.ResourceData) error {
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

	if c.featureSupported(featureReplication) {
		columns = append(columns, "rolreplication")
		values = append(values, &roleReplication)
	}

	if c.featureSupported(featureRLS) {
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
	err := c.DB().QueryRow(roleSQL, roleID).Scan(values...)

	switch {
	case err == sql.ErrNoRows:
		log.Printf("[WARN] PostgreSQL ROLE (%s) not found", roleID)
		d.SetId("")
		return nil
	case err != nil:
		return errwrap.Wrapf("Error reading ROLE: {{err}}", err)
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
	d.Set(roleReplicationAttr, roleBypassRLS)
	d.Set(roleRolesAttr, pgArrayToSet(roleRoles))
	d.Set(roleSearchPathAttr, readSearchPath(roleConfig))
	d.Set(roleStatementTimeoutAttr, readStatementTimeout(roleConfig))

	d.SetId(roleName)

	password, err := readRolePassword(c, d, roleCanLogin)
	if err != nil {
		return err
	}

	d.Set(rolePasswordAttr, password)
	return nil
}

// readSearchPath searches for a search_path entry in the rolconfig array.
// In case no such value is present, it returns nil.
func readSearchPath(roleConfig pq.ByteaArray) []string {
	for _, v := range roleConfig {
		config := string(v)
		if strings.HasPrefix(config, roleSearchPathAttr) {
			var result = strings.Split(strings.TrimPrefix(config, roleSearchPathAttr+"="), ", ")
			return result
		}
	}
	return nil
}

// readStatementTimeout searches for a statement_timeout entry in the rolconfig array.
// In case no such value is present, it returns nil.
func readStatementTimeout(roleConfig pq.ByteaArray) int {
	for _, v := range roleConfig {
		config := string(v)
		if strings.HasPrefix(config, roleStatementTimeoutAttr) {
			var result = strings.Split(strings.TrimPrefix(config, roleStatementTimeoutAttr+"="), ", ")
			res, err := strconv.Atoi(result[0])
			if err != nil {
				fmt.Println(err)
			}
			return res
		}
	}
	return 0
}

// readRolePassword reads password either from Postgres if admin user is a superuser
// or only from Terraform state.
func readRolePassword(c *Client, d *schema.ResourceData, roleCanLogin bool) (string, error) {
	statePassword := d.Get(rolePasswordAttr).(string)

	// Role which cannot login does not have password in pg_shadow.
	// Also, if user specifies that admin is not a superuser we don't try to read pg_shadow
	// (only superuser can read pg_shadow)
	if !roleCanLogin || !c.config.Superuser {
		return statePassword, nil
	}

	// Otherwise we check if connected user is really a superuser
	// (in order to warn user instead of having a permission denied error)
	superuser, err := c.isSuperuser()
	if err != nil {
		return "", err
	}
	if !superuser {
		return "", fmt.Errorf(
			"could not read role password from Postgres as "+
				"connected user %s is not a SUPERUSER. "+
				"You can set `superuser = false` in the provider configuration "+
				"so it will not try to read the password from Postgres",
			c.config.getDatabaseUsername(),
		)
	}

	var rolePassword string
	err = c.DB().QueryRow("SELECT COALESCE(passwd, '') FROM pg_catalog.pg_shadow AS s WHERE s.usename = $1", d.Id()).Scan(&rolePassword)
	switch {
	case err == sql.ErrNoRows:
		// They don't have a password
		return "", nil
	case err != nil:
		return "", errwrap.Wrapf("Error reading role: {{err}}", err)
	}

	// If the password isn't already in md5 format, but hashing the input
	// matches the password in the database for the user, they are the same
	if statePassword != "" && !strings.HasPrefix(statePassword, "md5") {
		hasher := md5.New()
		hasher.Write([]byte(statePassword + d.Id()))
		hashedPassword := "md5" + hex.EncodeToString(hasher.Sum(nil))

		if hashedPassword == rolePassword {
			// The passwords are actually the same
			// make Terraform think they are the same
			return statePassword, nil
		}
	}
	return rolePassword, nil
}

func resourcePostgreSQLRoleUpdate(d *schema.ResourceData, meta interface{}) error {
	c := meta.(*Client)
	c.catalogLock.Lock()
	defer c.catalogLock.Unlock()

	txn, err := c.DB().Begin()
	if err != nil {
		return err
	}
	defer deferredRollback(txn)

	if err := setRoleName(txn, d); err != nil {
		return err
	}

	if err := setRolePassword(txn, d); err != nil {
		return err
	}

	if err := setRoleBypassRLS(c, txn, d); err != nil {
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

	if err = txn.Commit(); err != nil {
		return errwrap.Wrapf("could not commit transaction: {{err}}", err)
	}

	return resourcePostgreSQLRoleReadImpl(c, d)
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
		return errwrap.Wrapf("Error updating role NAME: {{err}}", err)
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
		return errwrap.Wrapf("Error updating role password: {{err}}", err)
	}
	return nil
}

func setRoleBypassRLS(c *Client, txn *sql.Tx, d *schema.ResourceData) error {
	if !d.HasChange(roleBypassRLSAttr) {
		return nil
	}

	if !c.featureSupported(featureRLS) {
		return fmt.Errorf("PostgreSQL client is talking with a server (%q) that does not support PostgreSQL Row-Level Security", c.version.String())
	}

	bypassRLS := d.Get(roleBypassRLSAttr).(bool)
	tok := "NOBYPASSRLS"
	if bypassRLS {
		tok = "BYPASSRLS"
	}
	roleName := d.Get(roleNameAttr).(string)
	sql := fmt.Sprintf("ALTER ROLE %s WITH %s", pq.QuoteIdentifier(roleName), tok)
	if _, err := txn.Exec(sql); err != nil {
		return errwrap.Wrapf("Error updating role BYPASSRLS: {{err}}", err)
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
		return errwrap.Wrapf("Error updating role CONNECTION LIMIT: {{err}}", err)
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
		return errwrap.Wrapf("Error updating role CREATEDB: {{err}}", err)
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
		return errwrap.Wrapf("Error updating role CREATEROLE: {{err}}", err)
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
		return errwrap.Wrapf("Error updating role INHERIT: {{err}}", err)
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
		return errwrap.Wrapf("Error updating role LOGIN: {{err}}", err)
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
		return errwrap.Wrapf("Error updating role REPLICATION: {{err}}", err)
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
		return errwrap.Wrapf("Error updating role SUPERUSER: {{err}}", err)
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
		return errwrap.Wrapf("Error updating role VALID UNTIL: {{err}}", err)
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
		return errwrap.Wrapf(fmt.Sprintf("could not get roles list for role %s: {{err}}", role), err)
	}
	defer rows.Close()

	grantedRoles := []string{}
	for rows.Next() {
		var grantedRole string

		if err = rows.Scan(&grantedRole); err != nil {
			return errwrap.Wrapf(fmt.Sprintf("could not scan role name for role %s: {{err}}", role), err)
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
			return errwrap.Wrapf(fmt.Sprintf("could not revoke role %s from %s: {{err}}", string(grantedRole), role), err)
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
			return errwrap.Wrapf(fmt.Sprintf("could not grant role %s to %s: {{err}}", grantingRole, role), err)
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
		return errwrap.Wrapf(fmt.Sprintf("could not set search_path %s for %s: {{err}}", searchPath, role), err)
	}
	return nil
}

func setStatementTimeout(txn *sql.Tx, d *schema.ResourceData) error {
	if !d.HasChange(roleStatementTimeoutAttr) {
		return nil
	}

	roleName := d.Get(roleNameAttr).(string)
	statementTimeout := d.Get(roleStatementTimeoutAttr).(int)
	sql := fmt.Sprintf(
		"ALTER ROLE %s SET statement_timeout TO %d", pq.QuoteIdentifier(roleName), statementTimeout,
	)
	if _, err := txn.Exec(sql); err != nil {
		return errwrap.Wrapf(fmt.Sprintf("could not set statementtimeout %d for %s: {{err}}", statementTimeout, roleName), err)
	}

	return nil
}
