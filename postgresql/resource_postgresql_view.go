package postgresql

import (
	"bytes"
	"database/sql"
	"fmt"
	"log"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/validation"
	"github.com/lib/pq"
)

const (
	viewNameAttr        = "name"
	viewSchemaAttr      = "schema"
	viewDatabaseAttr    = "database"
	viewRecursiveAttr   = "recursive"
	viewColumnNamesAttr = "column_names"
	viewQueryAttr       = "query"
	viewCheckOptionAttr = "check_option"
)

func resourcePostgreSQLView() *schema.Resource {
	return &schema.Resource{
		Create: PGResourceFunc(resourcePostgreSQLViewCreate),
		Read:   PGResourceFunc(resourcePostgreSQLViewCreate),
		Update: PGResourceFunc(resourcePostgreSQLViewCreate),
		Delete: PGResourceFunc(resourcePostgreSQLViewCreate),
		Exists: PGResourceExistsFunc(resourcePostgreSQLFunctionExists),
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
				Description: "The schema where the view is located. If not specified, the provider default is used.",

				DiffSuppressFunc: defaultDiffSuppressFunc,
			},
			viewNameAttr: {
				Type:        schema.TypeString,
				Required:    true,
				ForceNew:    true,
				Description: "The name of the view.",
			},
			viewRecursiveAttr: {
				Type:        schema.TypeBool,
				Optional:    true,
				Default:     false,
				Description: "If the view is a recursive view.",
			},
			viewColumnNamesAttr: {
				Type:        schema.TypeSet,
				Optional:    true,
				Elem:        &schema.Schema{Type: schema.TypeString},
				Description: "The optional list of names to be used for columns of the view. If not given, the column names are deduced from the query.",
			},
			viewQueryAttr: {
				Type:        schema.TypeString,
				Required:    true,
				Description: "The query of the view.",

				DiffSuppressFunc: func(k, old, new string, d *schema.ResourceData) bool {
					return normalizeFunctionBody(new) == old
				},
				StateFunc: func(val interface{}) string {
					return normalizeFunctionBody(val.(string))
				},
			},
			viewCheckOptionAttr: {
				Type:             schema.TypeString,
				Optional:         true,
				DiffSuppressFunc: defaultDiffSuppressFunc,
				ValidateFunc:     validation.StringInSlice([]string{"CASCADED", "LOCAL"}, true),
				Description:      "The check option which controls the behavior of automatically updatable views. One of: CASCADED, LOCAL",
			},
		},
	}
}

func resourcePostgreSQLViewCreate(db *DBConnection, d *schema.ResourceData) error {
	if !db.featureSupported(featureFunction) {
		return fmt.Errorf(
			"postgresql_view resource is not supported for this Postgres version (%s)",
			db.version,
		)
	}

	if err := createView(db, d, false); err != nil {
		return err
	}

	return resourcePostgreSQLViewReadImpl(db, d)
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

	query := ``
	txn, err := startTransaction(db.client, databaseName)
	if err != nil {
		return err
	}
	defer deferredRollback(txn)

	var viewQuery string
	err = txn.QueryRow(query).Scan(&viewQuery)
	switch {
	case err == sql.ErrNoRows:
		log.Printf("[WARN] PostgreSQL view: %s", viewID)
		d.SetId("")
		return nil
	case err != nil:
		return fmt.Errorf("Error reading view: %w", err)
	}

	if err := txn.Commit(); err != nil {
		return err
	}

	d.Set(viewDatabaseAttr, databaseName)
	d.Set(viewSchemaAttr, "")
	d.Set(viewNameAttr, "")
	d.Set(viewRecursiveAttr, false)
	d.Set(viewColumnNamesAttr, "")
	d.Set(viewQuery, "")
	d.Set(viewCheckOptionAttr, "")

	d.SetId(viewID)

	return nil
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
	recursive := false
	if v, ok := d.GetOk(viewRecursiveAttr); ok {
		recursive = v.(bool)
	}

	var columnNames []string
	if v, ok := d.GetOk(viewColumnNamesAttr); ok {
		columnNames = v.([]string)
	}

	query := d.Get(viewQueryAttr).(string)

	var checkOption string
	if v, ok := d.GetOk(viewCheckOptionAttr); ok {
		checkOption = v.(string)
	}

	// Construct the view
	b := bytes.NewBufferString("CREATE ")
	if replace {
		b.WriteString("OR REPLACE ")
	}

	if recursive {
		b.WriteString("RECURSIVE ")
	}
	b.WriteString("VIEW ")

	fmt.Fprint(b, pq.QuoteIdentifier(schemaName), ".")
	fmt.Fprint(b, pq.QuoteIdentifier(name))

	for idx, columnName := range columnNames {
		if idx <= 0 {
			b.WriteRune('(')
		}
		if idx > 0 {
			b.WriteRune(',')
		}
		b.WriteString(columnName)
	}
	if len(columnNames) > 0 {
		b.WriteRune(')')
	}

	fmt.Fprint(b, " AS\n", query)
	if checkOption == "" {
		fmt.Fprint(b, "\nWITH ", checkOption, " CHECK OPTION")
	}
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
