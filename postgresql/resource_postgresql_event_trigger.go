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
	eventTriggerFunctionSchemaAttr = "function_schema"
	eventTriggerFilterAttr         = "filter"
	eventTriggerFilterVariableAttr = "variable"
	eventTriggerFilterValueAttr    = "values"
	eventTriggerDatabaseAttr       = "database"
	eventTriggerOwnerAttr          = "owner"
	eventTriggerStatusAttr         = "status"
)

func resourcePostgreSQLEventTrigger() *schema.Resource {
	return &schema.Resource{
		Create: PGResourceFunc(resourcePostgreSQLEventTriggerCreate),
		Read:   PGResourceFunc(resourcePostgreSQLEventTriggerRead),
		Update: PGResourceFunc(resourcePostgreSQLEventTriggerUpdate),
		Delete: PGResourceFunc(resourcePostgreSQLEventTriggerDelete),
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
			eventTriggerFunctionSchemaAttr: {
				Type:        schema.TypeString,
				Required:    true,
				ForceNew:    true,
				Description: "Schema where the function is located.",
			},
			eventTriggerStatusAttr: {
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
	eventTriggerName := d.Get(eventTriggerNameAttr).(string)
	d.SetId(eventTriggerName)

	b := bytes.NewBufferString("CREATE EVENT TRIGGER ")
	fmt.Fprint(b, pq.QuoteIdentifier(eventTriggerName))

	fmt.Fprint(b, " ON ", d.Get(eventTriggerOnAttr).(string))

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
	eventTriggerSchema := d.Get(eventTriggerFunctionSchemaAttr).(string)
	fmt.Fprint(b, " EXECUTE FUNCTION ", pq.QuoteIdentifier(eventTriggerSchema), ".", eventTriggerFunction, "()")

	createSql := b.String()

	// Enable or disable the event trigger
	b = bytes.NewBufferString("ALTER EVENT TRIGGER ")
	fmt.Fprint(b, pq.QuoteIdentifier(eventTriggerName))

	eventTriggerEnabled := d.Get(eventTriggerStatusAttr).(string)
	fmt.Fprint(b, " ", eventTriggerEnabled)

	statusSql := b.String()

	// Table owner
	b = bytes.NewBufferString("ALTER EVENT TRIGGER ")
	eventTriggerOwner := d.Get(eventTriggerOwnerAttr).(string)
	fmt.Fprint(b, pq.QuoteIdentifier(eventTriggerName), " OWNER TO ", pq.QuoteIdentifier(eventTriggerOwner))

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

func resourcePostgreSQLEventTriggerUpdate(db *DBConnection, d *schema.ResourceData) error {
	eventTriggerName := d.Get(eventTriggerNameAttr).(string)
	d.SetId(eventTriggerName)

	// Enable or disable the event trigger
	b := bytes.NewBufferString("ALTER EVENT TRIGGER ")
	fmt.Fprint(b, pq.QuoteIdentifier(eventTriggerName))

	eventTriggerEnabled := d.Get(eventTriggerStatusAttr).(string)
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

func resourcePostgreSQLEventTriggerDelete(db *DBConnection, d *schema.ResourceData) error {
	eventTriggerName := d.Get(eventTriggerNameAttr).(string)
	d.SetId(eventTriggerName)

	sql := fmt.Sprintf("DROP EVENT TRIGGER %s", pq.QuoteIdentifier(eventTriggerName))

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

func resourcePostgreSQLEventTriggerRead(db *DBConnection, d *schema.ResourceData) error {
	database, eventTriggerName, err := getDBEventTriggerName(d, db.client.databaseName)
	if err != nil {
		return err
	}

	query := `SELECT evtname, evtevent, proname, nspname, evtenabled, evttags, pg_get_userbyid(evtowner) ` +
		`FROM pg_catalog.pg_event_trigger ` +
		`JOIN pg_catalog.pg_proc on pg_catalog.pg_event_trigger.evtfoid = pg_catalog.pg_proc.oid ` +
		`JOIN pg_catalog.pg_namespace on pg_catalog.pg_proc.pronamespace = pg_catalog.pg_namespace.oid ` +
		`WHERE evtname=$1`

	var name, on, owner, function, schema, status string
	var tags []string

	values := []interface{}{
		&name,
		&on,
		&function,
		&schema,
		&status,
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
	d.Set(eventTriggerNameAttr, name)
	d.Set(eventTriggerOnAttr, on)
	d.Set(eventTriggerFunctionAttr, function)
	d.Set(eventTriggerOwnerAttr, owner)
	d.Set(eventTriggerDatabaseAttr, database)
	d.Set(eventTriggerFunctionSchemaAttr, schema)

	switch status {
	case "D":
		d.Set(eventTriggerStatusAttr, "disable")
	case "O":
		d.Set(eventTriggerStatusAttr, "enable")
	case "R":
		d.Set(eventTriggerStatusAttr, "enable_replica")
	case "A":
		d.Set(eventTriggerStatusAttr, "enable_always")
	}

	var filters []interface{}

	if len(tags) > 0 {
		var values []string
		values = append(values, tags...)

		filter := map[string]interface{}{
			"variable": "TAG",
			"values":   values,
		}

		filters = append(filters, filter)
	}

	d.Set(eventTriggerFilterAttr, filters)

	return nil
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
