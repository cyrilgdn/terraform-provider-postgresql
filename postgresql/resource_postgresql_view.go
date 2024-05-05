package postgresql

import (
	"bytes"
	"database/sql"
	"fmt"
	"log"
	"strconv"
	"strings"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/validation"
	"github.com/lib/pq"
)

const (
	viewNameAttr                = "name"
	viewSchemaAttr              = "schema"
	viewDatabaseAttr            = "database"
	viewQueryAttr               = "query"
	viewWithCheckOptionAttr     = "with_check_option"
	viewWithSecurityBarrierAttr = "with_security_barrier"
	viewWithSecurityInvokerAttr = "with_security_invoker"

	viewDropCascadeAttr = "drop_cascade"

	// The private attributes for storing internal states of a view
	internalStatesAttr = "internal_states"
)

func resourcePostgreSQLView() *schema.Resource {
	return &schema.Resource{
		Create: PGResourceFunc(resourcePostgreSQLViewCreate),
		Read:   PGResourceFunc(resourcePostgreSQLViewRead),
		Update: PGResourceFunc(resourcePostgreSQLViewUpdate),
		Delete: PGResourceFunc(resourcePostgreSQLViewDelete),
		Exists: PGResourceExistsFunc(resourcePostgreSQLViewExists),
		Importer: &schema.ResourceImporter{
			StateContext: schema.ImportStatePassthroughContext,
		},

		Schema: map[string]*schema.Schema{
			viewDatabaseAttr: {
				Type:        schema.TypeString,
				Optional:    true,
				Computed:    true,
				ForceNew:    true,
				Description: "The database where the view is located. If not specified, the provider default database is used.",

				DiffSuppressFunc: defaultDiffSuppressFunc,
			},
			viewSchemaAttr: {
				Type:        schema.TypeString,
				Optional:    true,
				Computed:    true,
				ForceNew:    true,
				Description: "The schema where the view is located. If not specified, the provider default schema is used.",

				DiffSuppressFunc: defaultDiffSuppressFunc,
			},
			viewNameAttr: {
				Type:        schema.TypeString,
				Required:    true,
				ForceNew:    true,
				Description: "The name of the view.",
			},
			viewQueryAttr: {
				Type:        schema.TypeString,
				Required:    true,
				Description: "The query of the view.",

				DiffSuppressFunc: func(k, old, new string, d *schema.ResourceData) bool {
					internalStates := d.Get(internalStatesAttr).(map[string]interface{})
					return internalStates["original_tf_view_query"] == new && internalStates["original_pg_view_query"] == old
				},
				StateFunc: func(val interface{}) string {
					return val.(string)
				},
			},
			viewWithCheckOptionAttr: {
				Type:             schema.TypeString,
				Optional:         true,
				DiffSuppressFunc: defaultDiffSuppressFunc,
				ValidateFunc:     validation.StringInSlice([]string{"CASCADED", "LOCAL"}, true),
				Description:      "The check option which controls the behavior of automatically updatable views. One of: CASCADED, LOCAL",
			},
			viewWithSecurityBarrierAttr: {
				Type:        schema.TypeBool,
				Optional:    true,
				Default:     false,
				Description: "This should be used if the view is intended to provide row-level security.",
			},
			viewWithSecurityInvokerAttr: {
				Type:        schema.TypeBool,
				Optional:    true,
				Default:     false,
				Description: "This option causes the underlying base relations to be checked against the privileges of the user of the view rather than the view owner.",
			},
			viewDropCascadeAttr: {
				Type:        schema.TypeBool,
				Description: "Automatically drop objects that depend on the view (such as other views), and in turn all objects that depend on those objects.",
				Optional:    true,
				Default:     false,
			},
			internalStatesAttr: {
				Type:     schema.TypeMap,
				Computed: true,
			},
		},
	}
}

func resourcePostgreSQLViewCreate(db *DBConnection, d *schema.ResourceData) error {
	if !db.featureSupported(featureView) {
		return fmt.Errorf(
			"postgresql_view resource is not supported for this Postgres version (%s)",
			db.version,
		)
	}

	originalViewQuery := d.Get(viewQueryAttr).(string)

	if err := createView(db, d, false); err != nil {
		return err
	}

	if err := resourcePostgreSQLViewReadImpl(db, d); err != nil {
		return err
	}

	// Set internal states
	pgViewQuery := d.Get(viewQueryAttr).(string)
	internalStates := map[string]interface{}{
		"original_tf_view_query": originalViewQuery,
		"original_pg_view_query": pgViewQuery,
	}
	d.Set(internalStatesAttr, internalStates)
	return nil
}

func resourcePostgreSQLViewRead(db *DBConnection, d *schema.ResourceData) error {
	if !db.featureSupported(featureView) {
		return fmt.Errorf(
			"postgresql_view resource is not supported for this Postgres version (%s)",
			db.version,
		)
	}

	return resourcePostgreSQLViewReadImpl(db, d)
}

func resourcePostgreSQLViewUpdate(db *DBConnection, d *schema.ResourceData) error {
	if !db.featureSupported(featureView) {
		return fmt.Errorf(
			"postgresql_view resource is not supported for this Postgres version (%s)",
			db.version,
		)
	}

	originalViewQuery := d.Get(viewQueryAttr).(string)

	if err := createView(db, d, true); err != nil {
		return err
	}

	if err := resourcePostgreSQLViewReadImpl(db, d); err != nil {
		return err
	}

	// Set internal states
	pgViewQuery := d.Get(viewQueryAttr).(string)
	internalStates := map[string]interface{}{
		"original_tf_view_query": originalViewQuery,
		"original_pg_view_query": pgViewQuery,
	}
	d.Set(internalStatesAttr, internalStates)
	return nil
}

func resourcePostgreSQLViewDelete(db *DBConnection, d *schema.ResourceData) error {
	if !db.featureSupported(featureView) {
		return fmt.Errorf(
			"postgresql_view resource is not supported for this Postgres version (%s)",
			db.version,
		)
	}

	dropMode := "RESTRICT"
	if v, ok := d.GetOk(viewDropCascadeAttr); ok && v.(bool) {
		dropMode = "CASCADE"
	}

	viewParts := strings.Split(d.Id(), ".")
	databaseName, schemaName, viewName := viewParts[0], viewParts[1], viewParts[2]
	viewIdentifier := fmt.Sprintf("%s.%s", pq.QuoteIdentifier(schemaName), pq.QuoteIdentifier(viewName))

	sql := fmt.Sprintf("DROP VIEW IF EXISTS %s %s", viewIdentifier, dropMode)
	txn, err := startTransaction(db.client, databaseName)
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

	d.SetId("")

	return nil
}

func resourcePostgreSQLViewExists(db *DBConnection, d *schema.ResourceData) (bool, error) {
	if !db.featureSupported(featureView) {
		return false, fmt.Errorf(
			"postgresql_view resource is not supported for this Postgres version (%s)",
			db.version,
		)
	}

	viewParts := strings.Split(d.Id(), ".")
	databaseName, schemaName, viewName := viewParts[0], viewParts[1], viewParts[2]
	viewIdentifier := fmt.Sprintf("%s.%s", pq.QuoteIdentifier(schemaName), pq.QuoteIdentifier(viewName))

	var viewExists bool

	txn, err := startTransaction(db.client, databaseName)
	if err != nil {
		return false, err
	}
	defer deferredRollback(txn)

	query := fmt.Sprintf("SELECT to_regclass('%s') IS NOT NULL AS viewExists", viewIdentifier)

	if err := txn.QueryRow(query).Scan(&viewExists); err != nil {
		return false, err
	}

	if err := txn.Commit(); err != nil {
		return false, err
	}

	return viewExists, nil
}

type PGView struct {
	Database            string
	Schema              string
	Name                string
	Query               string
	WithCheckOption     string
	WithSecurityBarrier bool
	WithSecurityInvoker bool

	DropCascade bool
}

type ViewInfo struct {
	Database string   `db:"database"`
	Schema   string   `db:"schema"`
	Name     string   `db:"name"`
	Query    string   `db:"query"`
	Options  []string `db:"options"`
}

func resourcePostgreSQLViewReadImpl(db *DBConnection, d *schema.ResourceData) error {
	viewID := d.Id()
	if viewID == "" {
		genViewID, err := genViewID(db, d)
		if err != nil {
			return err
		}
		viewID = genViewID
	}

	// Query the view definition
	databaseName := db.client.databaseName
	if databaseAttr, ok := d.GetOk(viewDatabaseAttr); ok {
		databaseName = databaseAttr.(string)
	}

	query := `SELECT current_database() AS database, ` +
		`n.nspname AS schema, ` +
		`c.relname AS name, ` +
		`pg_get_viewdef(c.oid, true) AS query, ` +
		`c.reloptions AS options ` +
		`FROM pg_class c ` +
		`JOIN pg_namespace n ON c.relnamespace = n.oid ` +
		`WHERE c.relkind = 'v' AND n.nspname = $1 AND c.relname = $2`
	txn, err := startTransaction(db.client, databaseName)
	if err != nil {
		return err
	}
	defer deferredRollback(txn)

	viewIDParts := strings.Split(viewID, ".")
	var viewInfo ViewInfo
	err = txn.QueryRow(query, viewIDParts[1], viewIDParts[2]).Scan(&viewInfo.Database, &viewInfo.Schema, &viewInfo.Name, &viewInfo.Query, pq.Array(&viewInfo.Options))
	switch {
	case err == sql.ErrNoRows:
		log.Printf("[WARN] PostgreSQL view: %s", viewID)
		d.SetId("")
		return nil
	case err != nil:
		return fmt.Errorf("error reading view: %w", err)
	}

	if err := txn.Commit(); err != nil {
		return err
	}

	pgView, err := parseView(viewInfo)
	if err != nil {
		return err
	}

	d.Set(viewDatabaseAttr, pgView.Database)
	d.Set(viewSchemaAttr, pgView.Schema)
	d.Set(viewNameAttr, pgView.Name)
	d.Set(viewQueryAttr, pgView.Query)
	d.Set(viewWithCheckOptionAttr, pgView.WithCheckOption)
	d.Set(viewWithSecurityBarrierAttr, pgView.WithSecurityBarrier)
	d.Set(viewWithSecurityInvokerAttr, pgView.WithSecurityInvoker)
	if dropCascadeAttr, ok := d.GetOk(viewDropCascadeAttr); ok {
		d.Set(viewDropCascadeAttr, dropCascadeAttr.(bool))
	}

	d.SetId(viewID)

	return nil
}

func parseView(viewInfo ViewInfo) (PGView, error) {
	var pgView PGView
	pgView.Database = viewInfo.Database
	pgView.Schema = viewInfo.Schema
	pgView.Name = viewInfo.Name
	pgView.Query = viewInfo.Query

	// Parse options. There are 3 options:
	// 1. check_option (enum) - [LOCAL | CASCADED]
	// 2. security_barrier (boolean)
	// 3. security_invoker (boolean)
	options := viewInfo.Options
	if len(options) > 0 {
		for _, option := range options {
			parts := strings.Split(option, "=")
			if len(parts) != 2 {
				return pgView, fmt.Errorf("invalid view option: %s", option)
			}
			switch parts[0] {
			case "check_option":
				pgView.WithCheckOption = strings.ToUpper(parts[1])
			case "security_barrier":
				val, _ := strconv.ParseBool(parts[1])
				pgView.WithSecurityBarrier = val
			case "security_invoker":
				val, _ := strconv.ParseBool(parts[1])
				pgView.WithSecurityInvoker = val
			default:
				log.Printf("[WARN] Unsupported option: %s", parts[0])
			}
		}
	}
	return pgView, nil
}

func genViewID(db *DBConnection, d *schema.ResourceData) (string, error) {
	// Generate with format: <database_name>.<schema_name>.<view_name>
	b := bytes.NewBufferString("")
	if databaseAttr, ok := d.GetOk(viewDatabaseAttr); ok {
		fmt.Fprint(b, databaseAttr.(string), ".")
	} else {
		fmt.Fprint(b, db.client.databaseName, ".")
	}

	schemaName := "public"
	if v, ok := d.GetOk(viewSchemaAttr); ok {
		schemaName = v.(string)
	}
	viewName := d.Get(viewNameAttr).(string)

	fmt.Fprint(b, schemaName, ".", viewName)
	return b.String(), nil
}

func createView(db *DBConnection, d *schema.ResourceData, replace bool) error {
	schemaName := "public"
	if v, ok := d.GetOk(viewSchemaAttr); ok {
		schemaName = v.(string)
	}

	name := d.Get(viewNameAttr).(string)
	query := d.Get(viewQueryAttr).(string)

	// Construct the view
	b := bytes.NewBufferString("CREATE ")
	if replace {
		b.WriteString("OR REPLACE ")
	}

	b.WriteString("VIEW ")

	fmt.Fprint(b, pq.QuoteIdentifier(schemaName), ".")
	fmt.Fprint(b, pq.QuoteIdentifier(name))

	// With options
	var withOptions []string
	if v, ok := d.GetOk(viewWithCheckOptionAttr); ok {
		withOptions = append(withOptions, fmt.Sprintf("check_option=%s", v.(string)))
	}
	if v, ok := d.GetOk(viewWithSecurityBarrierAttr); ok {
		withOptions = append(withOptions, fmt.Sprintf("security_barrier=%v", v.(bool)))
	}
	if v, ok := d.GetOk(viewWithSecurityInvokerAttr); ok {
		withOptions = append(withOptions, fmt.Sprintf("security_invoker=%v", v.(bool)))
	}
	if len(withOptions) > 0 {
		fmt.Fprint(b, "WITH (", strings.Join(withOptions[:], ","), ")")
	}

	// Query
	fmt.Fprint(b, " AS\n", query)
	b.WriteRune(';')

	sql := b.String()
	txn, err := startTransaction(db.client, d.Get(viewDatabaseAttr).(string))
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
