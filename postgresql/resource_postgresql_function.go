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
	funcArgsAttr        = "args"
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
				Description: "Schema where the function is located. If not specified, the provider default schema is used.",
			},
			funcNameAttr: {
				Type:        schema.TypeString,
				Required:    true,
				ForceNew:    true,
				Description: "Name of the function.",
			},
			funcArgsAttr: {
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
						},
						funcArgModeAttr: {
							Type:        schema.TypeString,
							Description: "The argument mode. One of: IN, OUT, INOUT, or VARIADIC",
							Optional:    true,
							Default:     "IN",
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
	if err := createFunction(db, d, false); err != nil {
		return err
	}

	return resourcePostgreSQLFunctionReadImpl(db, d)
}

func resourcePostgreSQLFunctionExists(db *DBConnection, d *schema.ResourceData) (bool, error) {
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
		return fmt.Errorf("Error deleting extension: %w", err)
	}

	d.SetId("")

	return nil
}

func resourcePostgreSQLFunctionUpdate(db *DBConnection, d *schema.ResourceData) error {
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

	if args, ok := d.GetOk(funcArgsAttr); ok {
		args := args.([]map[string]interface{})

		for i, arg := range args {
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
				fmt.Fprint(b, " DEFAULT ", v.(string))
			}
		}

		if len(args) > 0 {
			b.WriteRune('\n')
		}
	}

	fmt.Fprint(b, ")\n", d.Get(funcBodyAttr).(string))

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
		return fmt.Errorf("Error creating extension: %w", err)
	}

	return nil
}

func getFunctionSignature(d *schema.ResourceData) string {
	b := bytes.NewBufferString("")

	if v, ok := d.GetOk(funcSchemaAttr); ok {
		fmt.Fprint(b, pq.QuoteIdentifier(v.(string)), ".")
	}

	fmt.Fprint(b, pq.QuoteIdentifier(d.Get(funcNameAttr).(string)), "(")

	if args, ok := d.GetOk(funcArgsAttr); ok {
		argCount := 0

		for _, arg := range args.([]map[string]interface{}) {
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