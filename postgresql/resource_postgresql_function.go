package postgresql

import (
	"bytes"
	"database/sql"
	"fmt"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/lib/pq"
	"log"
)

const (
	funcNameAttr        = "name"
	funcSchemaAttr      = "schema"
	funcBodyAttr        = "body"
	funcArgAttr         = "arg"
	funcReturnsAttr     = "returns"
	funcDropCascadeAttr = "drop_cascade"

	funcArgTypeAttr    = "type"
	funcArgNameAttr    = "name"
	funcArgModeAttr    = "mode"
	funcArgDefaultAttr = "default"
)

func resourcePostgreSQLFunction() *schema.Resource {
	return &schema.Resource{
		Create: PGResourceFunc(resourcePostgreSQLFunctionCreate),
		Read:   PGResourceFunc(resourcePostgreSQLFunctionRead),
		Update: PGResourceFunc(resourcePostgreSQLFunctionUpdate),
		Delete: PGResourceFunc(resourcePostgreSQLFunctionDelete),
		Exists: PGResourceExistsFunc(resourcePostgreSQLFunctionExists),
		Importer: &schema.ResourceImporter{
			StateContext: schema.ImportStatePassthroughContext,
		},

		Schema: map[string]*schema.Schema{
			funcSchemaAttr: {
				Type:        schema.TypeString,
				Optional:    true,
				Computed:    true,
				ForceNew:    true,
				Description: "Schema where the function is located. If not specified, the provider default schema is used.",
			},
			funcNameAttr: {
				Type:        schema.TypeString,
				Required:    true,
				ForceNew:    true,
				Description: "Name of the function.",
			},
			funcArgAttr: {
				Type: schema.TypeList,
				Elem: &schema.Resource{
					Schema: map[string]*schema.Schema{
						funcArgTypeAttr: {
							Type:        schema.TypeString,
							Description: "The argument type.",
							Required:    true,
							ForceNew:    true,
						},
						funcArgNameAttr: {
							Type:        schema.TypeString,
							Description: "The argument name. The name may be required for some languages or depending on the argument mode.",
							Optional:    true,
							ForceNew:    true,
						},
						funcArgModeAttr: {
							Type:        schema.TypeString,
							Description: "The argument mode. One of: IN, OUT, INOUT, or VARIADIC",
							Optional:    true,
							Default:     "IN",
							ForceNew:    true,
						},
						funcArgDefaultAttr: {
							Type:        schema.TypeString,
							Description: "An expression to be used as default value if the parameter is not specified.",
							Optional:    true,
						},
					},
				},
				Optional:    true,
				ForceNew:    true,
				Description: "Function argument definitions.",
			},
			funcReturnsAttr: {
				Type:        schema.TypeString,
				Optional:    true,
				ForceNew:    true,
				Description: "Function return type.",
			},
			funcBodyAttr: {
				Type:        schema.TypeString,
				Required:    true,
				Description: "Body of the function.",
			},
			funcDropCascadeAttr: {
				Type:        schema.TypeBool,
				Description: "Automatically drop objects that depend on the function (such as operators or triggers), and in turn all objects that depend on those objects.",
				Optional:    true,
				Default:     false,
			},
		},
	}
}

func resourcePostgreSQLFunctionCreate(db *DBConnection, d *schema.ResourceData) error {
	if !db.featureSupported(featureFunction) {
		return fmt.Errorf(
			"postgresql_function resource is not supported for this Postgres version (%s)",
			db.version,
		)
	}

	if err := createFunction(db, d, false); err != nil {
		return err
	}

	return resourcePostgreSQLFunctionReadImpl(db, d)
}

func resourcePostgreSQLFunctionExists(db *DBConnection, d *schema.ResourceData) (bool, error) {
	if !db.featureSupported(featureFunction) {
		return false, fmt.Errorf(
			"postgresql_function resource is not supported for this Postgres version (%s)",
			db.version,
		)
	}

	signature := getFunctionSignature(d)
	functionExists := false

	txn, err := startTransaction(db.client, "")
	if err != nil {
		return false, err
	}
	defer deferredRollback(txn)

	query := fmt.Sprintf("SELECT to_regprocedure('%s') IS NOT NULL", signature)
	err = txn.QueryRow(query).Scan(&functionExists)
	return functionExists, err
}

func resourcePostgreSQLFunctionRead(db *DBConnection, d *schema.ResourceData) error {
	if !db.featureSupported(featureFunction) {
		return fmt.Errorf(
			"postgresql_function resource is not supported for this Postgres version (%s)",
			db.version,
		)
	}

	return resourcePostgreSQLFunctionReadImpl(db, d)
}

func resourcePostgreSQLFunctionReadImpl(db *DBConnection, d *schema.ResourceData) error {
	signature := getFunctionSignature(d)

	txn, err := startTransaction(db.client, "")
	if err != nil {
		return err
	}
	defer deferredRollback(txn)

	var funcSchema, funcName string

	query := `SELECT n.nspname, p.proname ` +
		`FROM pg_proc p ` +
		`LEFT JOIN pg_namespace n ON p.pronamespace = n.oid ` +
		`WHERE p.oid = to_regprocedure($1)`
	err = txn.QueryRow(query, signature).Scan(&funcSchema, &funcName)
	switch {
	case err == sql.ErrNoRows:
		log.Printf("[WARN] PostgreSQL function: %s", signature)
		d.SetId("")
		return nil
	case err != nil:
		return fmt.Errorf("Error reading function: %w", err)
	}

	d.Set(funcNameAttr, funcName)
	d.Set(funcSchemaAttr, funcSchema)
	d.SetId(signature)

	return nil
}

func resourcePostgreSQLFunctionDelete(db *DBConnection, d *schema.ResourceData) error {
	if !db.featureSupported(featureFunction) {
		return fmt.Errorf(
			"postgresql_function resource is not supported for this Postgres version (%s)",
			db.version,
		)
	}

	signature := getFunctionSignature(d)

	txn, err := startTransaction(db.client, "")
	if err != nil {
		return err
	}
	defer deferredRollback(txn)

	dropMode := "RESTRICT"
	if v, ok := d.GetOk(funcDropCascadeAttr); ok && v.(bool) {
		dropMode = "CASCADE"
	}

	sql := fmt.Sprintf("DROP FUNCTION IF EXISTS %s %s", signature, dropMode)
	if _, err := txn.Exec(sql); err != nil {
		return err
	}

	if err = txn.Commit(); err != nil {
		return fmt.Errorf("Error deleting function: %w", err)
	}

	d.SetId("")

	return nil
}

func resourcePostgreSQLFunctionUpdate(db *DBConnection, d *schema.ResourceData) error {
	if !db.featureSupported(featureFunction) {
		return fmt.Errorf(
			"postgresql_function resource is not supported for this Postgres version (%s)",
			db.version,
		)
	}

	if err := createFunction(db, d, true); err != nil {
		return err
	}

	return resourcePostgreSQLFunctionReadImpl(db, d)
}

func createFunction(db *DBConnection, d *schema.ResourceData, replace bool) error {
	b := bytes.NewBufferString("CREATE ")

	if replace {
		b.WriteString(" OR REPLACE ")
	}

	b.WriteString("FUNCTION ")

	if v, ok := d.GetOk(funcSchemaAttr); ok {
		fmt.Fprint(b, pq.QuoteIdentifier(v.(string)), ".")
	}

	fmt.Fprint(b, pq.QuoteIdentifier(d.Get(funcNameAttr).(string)), " (")

	if args, ok := d.GetOk(funcArgAttr); ok {
		args := args.([]interface{})

		for i, arg := range args {
			arg := arg.(map[string]interface{})

			if i > 0 {
				b.WriteRune(',')
			}

			b.WriteString("\n    ")

			if v, ok := arg[funcArgModeAttr]; ok {
				fmt.Fprint(b, v.(string), " ")
			}

			if v, ok := arg[funcArgNameAttr]; ok {
				fmt.Fprint(b, v.(string), " ")
			}

			b.WriteString(arg[funcArgTypeAttr].(string))

			if v, ok := arg[funcArgDefaultAttr]; ok {
				v := v.(string)

				if len(v) > 0 {
					fmt.Fprint(b, " DEFAULT ", v)
				}
			}
		}

		if len(args) > 0 {
			b.WriteRune('\n')
		}
	}

	b.WriteString(")")

	if v, ok := d.GetOk(funcReturnsAttr); ok {
		fmt.Fprint(b, " RETURNS ", v.(string))
	}

	fmt.Fprint(b, "\n", d.Get(funcBodyAttr).(string))

	txn, err := startTransaction(db.client, "")
	if err != nil {
		return err
	}
	defer deferredRollback(txn)

	sql := b.String()
	if _, err := txn.Exec(sql); err != nil {
		return err
	}

	if err = txn.Commit(); err != nil {
		return fmt.Errorf("Error creating function: %w", err)
	}

	return nil
}

func getFunctionSignature(d *schema.ResourceData) string {
	b := bytes.NewBufferString("")

	if v, ok := d.GetOk(funcSchemaAttr); ok {
		fmt.Fprint(b, pq.QuoteIdentifier(v.(string)), ".")
	}

	fmt.Fprint(b, pq.QuoteIdentifier(d.Get(funcNameAttr).(string)), "(")

	if args, ok := d.GetOk(funcArgAttr); ok {
		argCount := 0

		for _, arg := range args.([]interface{}) {
			arg := arg.(map[string]interface{})

			mode := "IN"

			if v, ok := arg[funcArgModeAttr]; ok {
				mode = v.(string)
			}

			if mode != "OUT" {
				if argCount > 0 {
					b.WriteRune(',')
				}

				b.WriteString(arg[funcArgTypeAttr].(string))
				argCount += 1
			}
		}
	}

	b.WriteRune(')')

	return b.String()
}
