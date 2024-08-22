package postgresql

import (
	"bytes"
	"database/sql"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/validation"
	"github.com/lib/pq"
)

const (
	eventTriggerNameAttr           = "name"
	eventTriggerOnAttr             = "on"
	eventTriggerFunctionAttr       = "function"
	eventTriggerFilterAttr         = "filter"
	eventTriggerFilterVariableAttr = "variable"
	eventTriggerFilterValueAttr    = "values"
	eventTriggerDatabaseAttr       = "database"
	eventTriggerSchemaAttr         = "schema"
	eventTriggerOwnerAttr          = "owner"
	eventTriggerEnabledAttr        = "enabled"
)

func resourcePostgreSQLEventTrigger() *schema.Resource {
	return &schema.Resource{
		Create: PGResourceFunc(resourcePostgreSQLEventTriggerCreate),
		Read:   PGResourceFunc(resourcePostgreSQLEventTriggerRead),
		Update: PGResourceFunc(resourcePostgreSQLEventTriggerUpdate),
		Delete: PGResourceFunc(resourcePostgreSQLEventTriggerDelete),
		Exists: PGResourceExistsFunc(resourcePostgreSQLEventTriggerExists),
		Importer: &schema.ResourceImporter{
			StateContext: schema.ImportStatePassthroughContext,
		},

		Schema: map[string]*schema.Schema{
			eventTriggerNameAttr: {
				Type:        schema.TypeString,
				Required:    true,
				Description: "The name of the event trigger to create",
			},
			eventTriggerOnAttr: {
				Type:        schema.TypeString,
				Required:    true,
				ForceNew:    true,
				Description: "The event the trigger will listen to",
				ValidateFunc: validation.StringInSlice([]string{
					"ddl_command_start",
					"ddl_command_end",
					"sql_drop",
					"table_rewrite",
				}, false),
			},
			eventTriggerFunctionAttr: {
				Type:        schema.TypeString,
				Required:    true,
				ForceNew:    true,
				Description: "A function that is declared as taking no argument and returning type event_trigger",
			},
			eventTriggerFilterAttr: {
				Type:     schema.TypeList,
				Optional: true,
				ForceNew: true,
				Elem: &schema.Resource{
					Schema: map[string]*schema.Schema{
						eventTriggerFilterVariableAttr: {
							Type:        schema.TypeString,
							Required:    true,
							ForceNew:    true,
							Description: "The name of a variable used to filter events. Currently the only supported value is TAG",
							ValidateFunc: validation.StringInSlice([]string{
								"TAG",
							}, false),
						},

						eventTriggerFilterValueAttr: {
							Type:        schema.TypeList,
							Required:    true,
							ForceNew:    true,
							MinItems:    1,
							Description: "A list of values for the associated filter_variable for which the trigger should fire",
							Elem: &schema.Schema{
								Type: schema.TypeString,
							},
						},
					},
				},
			},
			eventTriggerDatabaseAttr: {
				Type:        schema.TypeString,
				Optional:    true,
				Computed:    true,
				ForceNew:    true,
				Description: "The database where the event trigger is located. If not specified, the provider default database is used.",
			},
			eventTriggerSchemaAttr: {
				Type:        schema.TypeString,
				Optional:    true,
				Computed:    true,
				ForceNew:    true,
				Description: "Schema where the function is located. If not specified, the provider default schema is used.",
			},
			eventTriggerEnabledAttr: {
				Type:        schema.TypeString,
				Optional:    true,
				Default:     "enable",
				Description: "These configure the firing of event triggers. A disabled trigger is still known to the system, but is not executed when its triggering event occurs",
				ValidateFunc: validation.StringInSlice([]string{
					"disable",
					"enable",
					"enable_replica",
					"enable_always",
				}, false),
			},
			eventTriggerOwnerAttr: {
				Type:        schema.TypeString,
				Required:    true,
				Description: "The user name of the owner of the event trigger. You can't use 'current_role', 'current_user' or 'session_user' in order to avoid drifts",
				ValidateFunc: validation.StringNotInSlice([]string{
					"current_role",
					"current_user",
					"session_user",
				}, true),
			},
		},
	}
}

func resourcePostgreSQLEventTriggerCreate(db *DBConnection, d *schema.ResourceData) error {
	if err := createEventTrigger(db, d); err != nil {
		return err
	}

	d.SetId(d.Get(eventTriggerNameAttr).(string))

	return nil
}

func resourcePostgreSQLEventTriggerUpdate(db *DBConnection, d *schema.ResourceData) error {
	if err := updateEventTrigger(db, d); err != nil {
		return err
	}

	d.SetId(d.Get(eventTriggerNameAttr).(string))

	return nil
}

func resourcePostgreSQLEventTriggerDelete(db *DBConnection, d *schema.ResourceData) error {
	if err := deleteEventTrigger(db, d); err != nil {
		return err
	}

	d.SetId(d.Get(eventTriggerNameAttr).(string))

	return nil
}

func resourcePostgreSQLEventTriggerRead(db *DBConnection, d *schema.ResourceData) error {
	return readEventTrigger(db, d)
}

func resourcePostgreSQLEventTriggerExists(db *DBConnection, d *schema.ResourceData) (bool, error) {
	database, eventTriggerName, err := getDBEventTriggerName(d, db.client.databaseName)
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

	err = txn.QueryRow("SELECT evtname FROM pg_event_trigger WHERE evtname=$1", eventTriggerName).Scan(&eventTriggerName)
	switch {
	case err == sql.ErrNoRows:
		return false, nil
	case err != nil:
		return false, fmt.Errorf("Error reading schema: %w", err)
	}

	return true, nil
}

func getDBEventTriggerName(d *schema.ResourceData, databaseName string) (string, string, error) {
	database := getDatabase(d, databaseName)
	eventTriggerName := d.Get(eventTriggerNameAttr).(string)

	// When importing, we have to parse the ID to find event trigger and database names.
	if eventTriggerName == "" {
		parsed := strings.Split(d.Id(), ".")
		if len(parsed) != 2 {
			return "", "", fmt.Errorf("schema ID %s has not the expected format 'database.event_trigger': %v", d.Id(), parsed)
		}
		database = parsed[0]
		eventTriggerName = parsed[1]
	}

	return database, eventTriggerName, nil
}

func createEventTrigger(db *DBConnection, d *schema.ResourceData) error {
	eventTriggerName := d.Get(eventTriggerNameAttr).(string)
	b := bytes.NewBufferString("CREATE EVENT TRIGGER ")
	fmt.Fprint(b, pq.QuoteIdentifier(eventTriggerName))

	eventTriggerOn := d.Get(eventTriggerOnAttr).(string)
	fmt.Fprint(b, " ON ", eventTriggerOn)

	if filters, ok := d.GetOk(eventTriggerFilterAttr); ok {
		filters := filters.([]interface{})

		for i, filter := range filters {
			filter := filter.(map[string]interface{})

			if variable, ok := filter[eventTriggerFilterVariableAttr]; ok {
				if i == 0 {
					fmt.Fprint(b, " WHEN ", variable)
				} else {
					fmt.Fprint(b, " AND ", variable)
				}
			}

			if values, ok := filter[eventTriggerFilterValueAttr]; ok {
				var new_values []string

				for _, value := range values.([]interface{}) {
					new_values = append(new_values, pq.QuoteLiteral(value.(string)))
				}

				fmt.Fprint(b, " IN (", strings.Join(new_values, ","), ")")
			}
		}
	}

	eventTriggerFunction := d.Get(eventTriggerFunctionAttr).(string)
	eventTriggerSchema := d.Get(eventTriggerSchemaAttr).(string)
	fmt.Fprint(b, " EXECUTE FUNCTION ", pq.QuoteIdentifier(eventTriggerSchema), ".", eventTriggerFunction, "()")

	createSql := b.String()

	// Enable or disable the event trigger
	b = bytes.NewBufferString("ALTER EVENT TRIGGER ")
	fmt.Fprint(b, pq.QuoteIdentifier(eventTriggerName))

	eventTriggerEnabled := d.Get(eventTriggerEnabledAttr).(string)
	fmt.Fprint(b, " ", eventTriggerEnabled)

	statusSql := b.String()

	// Table owner
	b = bytes.NewBufferString("ALTER EVENT TRIGGER ")
	eventTriggerOwner := d.Get(eventTriggerOwnerAttr).(string)
	fmt.Fprint(b, pq.QuoteIdentifier(eventTriggerName), " OWNER TO ", eventTriggerOwner)

	ownerSql := b.String()

	// Start transaction
	txn, err := startTransaction(db.client, d.Get(eventTriggerDatabaseAttr).(string))
	if err != nil {
		return err
	}
	defer deferredRollback(txn)

	if _, err := txn.Exec(createSql); err != nil {
		return err
	}

	if _, err := txn.Exec(statusSql); err != nil {
		return err
	}

	if _, err := txn.Exec(ownerSql); err != nil {
		return err
	}

	if err := txn.Commit(); err != nil {
		return err
	}

	return nil
}

func updateEventTrigger(db *DBConnection, d *schema.ResourceData) error {
	eventTriggerName := d.Get(eventTriggerNameAttr).(string)

	// Enable or disable the event trigger
	b := bytes.NewBufferString("ALTER EVENT TRIGGER ")
	fmt.Fprint(b, pq.QuoteIdentifier(eventTriggerName))

	eventTriggerEnabled := d.Get(eventTriggerEnabledAttr).(string)
	fmt.Fprint(b, " ", eventTriggerEnabled)

	statusSql := b.String()

	// Table owner
	b = bytes.NewBufferString("ALTER EVENT TRIGGER ")
	eventTriggerOwner := d.Get(eventTriggerOwnerAttr).(string)
	fmt.Fprint(b, pq.QuoteIdentifier(eventTriggerName), " OWNER TO ", eventTriggerOwner)

	ownerSql := b.String()

	txn, err := startTransaction(db.client, d.Get(eventTriggerDatabaseAttr).(string))
	if err != nil {
		return err
	}
	defer deferredRollback(txn)

	if _, err := txn.Exec(statusSql); err != nil {
		return err
	}

	if _, err := txn.Exec(ownerSql); err != nil {
		return err
	}

	if err := txn.Commit(); err != nil {
		return err
	}

	return nil
}

func deleteEventTrigger(db *DBConnection, d *schema.ResourceData) error {
	eventTriggerName := d.Get(eventTriggerNameAttr).(string)
	b := bytes.NewBufferString("DROP EVENT TRIGGER ")
	fmt.Fprint(b, pq.QuoteIdentifier(eventTriggerName))

	sql := b.String()

	txn, err := startTransaction(db.client, d.Get(eventTriggerDatabaseAttr).(string))
	if err != nil {
		return err
	}
	defer deferredRollback(txn)

	if _, err := txn.Exec(sql); err != nil {
		return err
	}

	if err := txn.Commit(); err != nil {
		return err
	}

	return nil
}

func readEventTrigger(db *DBConnection, d *schema.ResourceData) error {
	database, eventTriggerName, err := getDBEventTriggerName(d, db.client.databaseName)
	if err != nil {
		return err
	}

	query := `SELECT evtname, evtevent, proname, nspname, evtenabled, evttags, usename ` +
		`FROM pg_catalog.pg_event_trigger ` +
		`JOIN pg_catalog.pg_user on pg_catalog.pg_event_trigger.evtowner = pg_catalog.pg_user.usesysid ` +
		`JOIN pg_catalog.pg_proc on pg_catalog.pg_event_trigger.evtfoid = pg_catalog.pg_proc.oid ` +
		`JOIN pg_catalog.pg_namespace on pg_catalog.pg_proc.pronamespace = pg_catalog.pg_namespace.oid ` +
		`WHERE evtname=$1`

	var name, on, owner, function, schema string
	var enabled string
	var tags []string

	values := []interface{}{
		&name,
		&on,
		&function,
		&schema,
		&enabled,
		(*pq.StringArray)(&tags),
		&owner,
	}

	txn, err := startTransaction(db.client, database)
	if err != nil {
		return err
	}
	defer deferredRollback(txn)

	err = txn.QueryRow(query, eventTriggerName).Scan(values...)
	switch {
	case err == sql.ErrNoRows:
		d.SetId("")
		return nil
	case err != nil:
		return fmt.Errorf("error reading event trigger: %w", err)
	}

	if err := txn.Commit(); err != nil {
		return err
	}

	d.SetId(name)
	d.Set("name", name)
	d.Set("on", on)
	d.Set("function", function)
	d.Set("owner", owner)
	d.Set("database", database)
	d.Set("schema", schema)

	switch enabled {
	case "D":
		d.Set("enabled", "disable")
	case "O":
		d.Set("enabled", "enable")
	case "R":
		d.Set("enabled", "enable_replica")
	case "A":
		d.Set("enabled", "enable_always")
	}

	// TODO: maybe it's better to add a struct
	// with types instead of an interface{}?
	var filters []interface{}

	if len(tags) > 0 {
		var values []string

		for _, tag := range tags {
			values = append(values, tag)
		}

		filter := map[string]interface{}{
			"variable": "TAG",
			"values":   values,
		}

		filters = append(filters, filter)
	}

	d.Set("filter", filters)

	return nil
}
