package postgresql

import (
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/validation"
	"github.com/lib/pq"
)

const (
	RoleName     = "role_name"
	SettingName  = "setting_name"
	SettingValue = "setting_value"
)

func resourceCockroachDBRoleSetting() *schema.Resource {
	return &schema.Resource{
		Create: PGResourceFunc(resourceCockroachDBRoleSettingCreate),
		Read:   PGResourceFunc(resourceCockroachDBRoleSettingRead),
		Update: PGResourceFunc(resourceCockroachDBRoleSettingUpdate),
		Delete: PGResourceFunc(resourceCockroachDBRoleSettingDelete),
		Exists: PGResourceExistsFunc(resourceCockroachDBRoleSettingExists),
		Importer: &schema.ResourceImporter{
			StateContext: schema.ImportStatePassthroughContext,
		},
		Schema: map[string]*schema.Schema{
			RoleName: {
				Type:         schema.TypeString,
				Required:     true,
				ForceNew:     true,
				ValidateFunc: validation.StringIsNotEmpty,
				Description:  "Role to apply the setting to. Use 'ALL' to apply to all roles.",
			},
			SettingName: {
				Type:         schema.TypeString,
				Required:     true,
				ForceNew:     true,
				ValidateFunc: validation.StringIsNotEmpty,
				Description:  "Role setting name (e.g. 'statement_timeout').",
			},
			SettingValue: {
				Type:         schema.TypeString,
				Required:     true,
				ValidateFunc: validation.StringIsNotEmpty,
				Description:  "Role setting value (e.g. '1m').",
			},
		},
	}
}

func resourceCockroachDBRoleSettingCreate(db *DBConnection, d *schema.ResourceData) error {
	roleName := d.Get(RoleName).(string)
	settingName := d.Get(SettingName).(string)
	settingValue := d.Get(SettingValue).(string)

	if err := execAlterRoleSet(db, roleName, settingName, settingValue); err != nil {
		return err
	}

	d.SetId(fmt.Sprintf("%s:%s", roleName, settingName))
	return nil
}

func resourceCockroachDBRoleSettingRead(db *DBConnection, d *schema.ResourceData) error {
	return resourceCockroachDBRoleSettingReadImpl(db, d)
}

func resourceCockroachDBRoleSettingReadImpl(db *DBConnection, d *schema.ResourceData) error {
	roleName, settingName, err := parseRoleSettingID(d)
	if err != nil {
		return err
	}

	value, found, err := readRoleSetting(db, roleName, settingName)
	if err != nil {
		return err
	}
	if !found {
		d.SetId("")
		return nil
	}

	d.Set(RoleName, roleName)
	d.Set(SettingName, settingName)
	d.Set(SettingValue, value)
	return nil
}

func resourceCockroachDBRoleSettingUpdate(db *DBConnection, d *schema.ResourceData) error {
	if d.HasChange(SettingValue) {
		roleName := d.Get(RoleName).(string)
		settingName := d.Get(SettingName).(string)
		settingValue := d.Get(SettingValue).(string)

		if err := execAlterRoleSet(db, roleName, settingName, settingValue); err != nil {
			return err
		}
	}
	return resourceCockroachDBRoleSettingReadImpl(db, d)
}

func resourceCockroachDBRoleSettingDelete(db *DBConnection, d *schema.ResourceData) error {
	roleName := d.Get(RoleName).(string)
	settingName := d.Get(SettingName).(string)

	roleRef := roleSettingRoleRef(roleName)
	query := fmt.Sprintf("ALTER ROLE %s RESET %s", roleRef, pq.QuoteIdentifier(settingName))
	if _, err := db.ExecRetry(query); err != nil {
		return fmt.Errorf("error resetting session setting %s for role %s: %w", settingName, roleName, err)
	}

	d.SetId("")
	return nil
}

func resourceCockroachDBRoleSettingExists(db *DBConnection, d *schema.ResourceData) (bool, error) {
	roleName, settingName, err := parseRoleSettingID(d)
	if err != nil {
		return false, err
	}

	_, found, err := readRoleSetting(db, roleName, settingName)
	return found, err
}

var numericSettingValue = regexp.MustCompile(`^-?[0-9]+(\.[0-9]+)?$`)

// roleSettingValueSQL renders the value for ALTER ROLE … SET. Numbers go unquoted:
// CockroachDB rejects a quoted value for integer settings such as transaction_rows_read_err.
func roleSettingValueSQL(settingValue string) string {
	if numericSettingValue.MatchString(settingValue) {
		return settingValue
	}
	return "'" + pqQuoteLiteral(settingValue) + "'"
}

// execAlterRoleSet runs ALTER ROLE <role> SET <setting> = <value>.
func execAlterRoleSet(db *DBConnection, roleName, settingName, settingValue string) error {
	roleRef := roleSettingRoleRef(roleName)
	query := fmt.Sprintf(
		"ALTER ROLE %s SET %s = %s",
		roleRef,
		pq.QuoteIdentifier(settingName),
		roleSettingValueSQL(settingValue),
	)
	if _, err := db.ExecRetry(query); err != nil {
		return fmt.Errorf("error setting %s for role %s: %w", settingName, roleName, err)
	}
	return nil
}

// roleSettingRoleRef formats the role reference for ALTER ROLE statements.
// "ALL" is a SQL keyword and must not be quoted as an identifier.
func roleSettingRoleRef(roleName string) string {
	if strings.EqualFold(roleName, "ALL") {
		return "ALL"
	}
	return pq.QuoteIdentifier(roleName)
}

func parseRoleSettingID(d *schema.ResourceData) (roleName, settingName string, err error) {
	roleName = d.Get(RoleName).(string)
	settingName = d.Get(SettingName).(string)

	// During import the fields are not yet populated; derive them from the ID.
	if roleName == "" || settingName == "" {
		parts := strings.SplitN(d.Id(), ":", 2)
		if len(parts) != 2 {
			return "", "", fmt.Errorf("invalid resource ID %q: expected <role_name>:<setting_name>", d.Id())
		}
		roleName = parts[0]
		settingName = parts[1]
	}
	return roleName, settingName, nil
}

// readRoleSetting uses SHOW DEFAULT SESSION VARIABLES FOR ROLE to look up the
// stored value for a specific setting. Only global settings (database IS NULL)
// are considered, matching the scope of ALTER ROLE … SET without IN DATABASE.
func readRoleSetting(db *DBConnection, roleName, settingName string) (value string, found bool, err error) {
	roleRef := roleSettingRoleRef(roleName)
	query := fmt.Sprintf(
		"SELECT default_values FROM [SHOW DEFAULT SESSION VARIABLES FOR ROLE %s] WHERE session_variables = $1 AND database IS NULL",
		roleRef,
	)
	err = db.QueryRowRetry(query, settingName).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("error reading role setting %s for role %s: %w", settingName, roleName, err)
	}
	return value, true, nil
}
