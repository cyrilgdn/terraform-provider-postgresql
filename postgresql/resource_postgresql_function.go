package postgresql

import (
	"bytes"
	"database/sql"
	"fmt"
	"log"
	"strings"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/validation"
	"github.com/lib/pq"
)

const (
	funcNameAttr            = "name"
	funcSchemaAttr          = "schema"
	funcBodyAttr            = "body"
	funcArgAttr             = "arg"
	funcLanguageAttr        = "language"
	funcReturnsAttr         = "returns"
	funcDropCascadeAttr     = "drop_cascade"
	funcDatabaseAttr        = "database"
	funcParallelAttr        = "parallel"
	funcSecurityDefinerAttr = "security_definer"
	funcStrictAttr          = "strict"
	funcVolatilityAttr      = "volatility"

	funcArgTypeAttr    = "type"
	funcArgNameAttr    = "name"
	funcArgModeAttr    = "mode"
	funcArgDefaultAttr = "default"

	defaultFunctionVolatility = "VOLATILE"
	defaultFunctionParallel   = "UNSAFE"
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

				DiffSuppressFunc: defaultDiffSuppressFunc,
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

							DiffSuppressFunc: func(k, old, new string, d *schema.ResourceData) bool {
								if (new == "" && old == "IN") || (new == "IN" && old == "") {
									return true
								}
								return old == new
							},
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
			funcLanguageAttr: {
				Type:        schema.TypeString,
				Optional:    true,
				ForceNew:    true,
				Default:     "plpgsql",
				Description: "Language of theof the function. One of: internal, sql, c, plpgsql",

				DiffSuppressFunc: defaultDiffSuppressFunc,
			},
			funcReturnsAttr: {
				Type:        schema.TypeString,
				Optional:    true,
				ForceNew:    true,
				Computed:    true,
				Description: "Function return type. If not specified, it will be calculated based on the output arguments",

				DiffSuppressFunc: defaultDiffSuppressFunc,
			},
			funcBodyAttr: {
				Type:        schema.TypeString,
				Required:    true,
				Description: "Body of the function.",

				DiffSuppressFunc: func(k, old, new string, d *schema.ResourceData) bool {
					return normalizeFunctionBody(new) == old
				},
				StateFunc: func(val interface{}) string {
					return normalizeFunctionBody(val.(string))
				},
			},
			funcDropCascadeAttr: {
				Type:        schema.TypeBool,
				Description: "Automatically drop objects that depend on the function (such as operators or triggers), and in turn all objects that depend on those objects.",
				Optional:    true,
				Default:     false,
			},
			funcParallelAttr: {
				Type:             schema.TypeString,
				Description:      "If the function can be executed in parallel for a single query execution. One of: UNSAFE, RESTRICTED, SAFE",
				Optional:         true,
				Default:          defaultFunctionParallel,
				DiffSuppressFunc: defaultDiffSuppressFunc,
				ValidateFunc:     validation.StringInSlice([]string{"UNSAFE", "RESTRICTED", "SAFE"}, false),
			},
			funcSecurityDefinerAttr: {
				Type:        schema.TypeBool,
				Description: "If the function should execute with the permissions of the function owner instead of the permissions of the caller.",
				Optional:    true,
				Default:     false,
			},
			funcStrictAttr: {
				Type:        schema.TypeBool,
				Description: "If the function should always return NULL if any of it's inputs is NULL.",
				Optional:    true,
				Default:     false,
			},
			funcVolatilityAttr: {
				Type:             schema.TypeString,
				Description:      "Volatility of the function. One of: VOLATILE, STABLE, IMMUTABLE.",
				Optional:         true,
				Default:          defaultFunctionVolatility,
				DiffSuppressFunc: defaultDiffSuppressFunc,
				ValidateFunc:     validation.StringInSlice([]string{"VOLATILE", "STABLE", "IMMUTABLE"}, false),
			},
			funcDatabaseAttr: {
				Type:        schema.TypeString,
				Optional:    true,
				Computed:    true,
				ForceNew:    true,
				Description: "The database where the function is located. If not specified, the provider default database is used.",

				DiffSuppressFunc: defaultDiffSuppressFunc,
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

	functionId := d.Id()

	databaseName, functionSignature, expandErr := expandFunctionID(functionId, d, db)
	if expandErr != nil {
		return false, expandErr
	}

	var functionExists bool

	txn, err := startTransaction(db.client, databaseName)
	if err != nil {
		return false, err
	}
	defer deferredRollback(txn)

	query := fmt.Sprintf("SELECT to_regprocedure('%s') IS NOT NULL AS functionExists", functionSignature)

	if err := txn.QueryRow(query).Scan(&functionExists); err != nil {
		return false, err
	}

	if err := txn.Commit(); err != nil {
		return false, err
	}

	return functionExists, nil
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
	functionId := d.Id()

	if functionId == "" {
		// Generate during creation
		generatedFunctionId, err := generateFunctionID(db, d)
		if err != nil {
			return err
		}
		functionId = generatedFunctionId
	}

	databaseName, functionSignature, expandErr := expandFunctionID(functionId, d, db)
	if expandErr != nil {
		return expandErr
	}

	var funcDefinition string

	query := `SELECT pg_get_functiondef(p.oid::regproc) funcDefinition ` +
		`FROM pg_proc p ` +
		`LEFT JOIN pg_namespace n ON p.pronamespace = n.oid ` +
		`WHERE p.oid = to_regprocedure($1)`

	txn, err := startTransaction(db.client, databaseName)
	if err != nil {
		return err
	}
	defer deferredRollback(txn)

	err = txn.QueryRow(query, functionSignature).Scan(&funcDefinition)
	switch {
	case err == sql.ErrNoRows:
		log.Printf("[WARN] PostgreSQL function: %s", functionId)
		d.SetId("")
		return nil
	case err != nil:
		return fmt.Errorf("Error reading function: %w", err)
	}

	if err := txn.Commit(); err != nil {
		return err
	}

	var pgFunction PGFunction

	err = pgFunction.Parse(funcDefinition)
	if err != nil {
		return err
	}

	var args []map[string]interface{}

	for _, a := range pgFunction.Args {
		args = append(args, map[string]interface{}{
			funcArgTypeAttr:    a.Type,
			funcArgNameAttr:    a.Name,
			funcArgModeAttr:    a.Mode,
			funcArgDefaultAttr: a.Default,
		})
	}

	d.Set(funcDatabaseAttr, databaseName)
	d.Set(funcNameAttr, pgFunction.Name)
	d.Set(funcSchemaAttr, pgFunction.Schema)
	d.Set(funcLanguageAttr, pgFunction.Language)
	d.Set(funcReturnsAttr, pgFunction.Returns)
	d.Set(funcBodyAttr, pgFunction.Body)
	d.Set(funcSecurityDefinerAttr, pgFunction.SecurityDefiner)
	d.Set(funcStrictAttr, pgFunction.Strict)
	d.Set(funcParallelAttr, pgFunction.Parallel)
	d.Set(funcVolatilityAttr, pgFunction.Volatility)
	d.Set(funcArgAttr, args)

	d.SetId(functionId)

	return nil
}

func resourcePostgreSQLFunctionDelete(db *DBConnection, d *schema.ResourceData) error {
	if !db.featureSupported(featureFunction) {
		return fmt.Errorf(
			"postgresql_function resource is not supported for this Postgres version (%s)",
			db.version,
		)
	}

	databaseName, functionSignature, err := expandFunctionID(d.Id(), d, db)
	if err != nil {
		return err
	}

	dropMode := "RESTRICT"
	if v, ok := d.GetOk(funcDropCascadeAttr); ok && v.(bool) {
		dropMode = "CASCADE"
	}

	sql := fmt.Sprintf("DROP FUNCTION IF EXISTS %s %s", functionSignature, dropMode)

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

	var pgFunction PGFunction
	err := pgFunction.FromResourceData(d)
	if err != nil {
		return err
	}

	b := bytes.NewBufferString("CREATE ")

	if replace {
		b.WriteString(" OR REPLACE ")
	}

	b.WriteString("FUNCTION ")

	fmt.Fprint(b, pq.QuoteIdentifier(pgFunction.Schema), ".")

	fmt.Fprint(b, pq.QuoteIdentifier(pgFunction.Name), " (")

	for i, arg := range pgFunction.Args {
		if i > 0 {
			b.WriteRune(',')
		}

		b.WriteString("\n    ")

		if arg.Mode != "" {
			fmt.Fprint(b, arg.Mode, " ")
		}

		if arg.Name != "" {
			fmt.Fprint(b, arg.Name, " ")
		}

		b.WriteString(arg.Type)

		if arg.Default != "" {
			fmt.Fprint(b, " DEFAULT ", arg.Default)
		}
	}

	if len(pgFunction.Args) > 0 {
		b.WriteRune('\n')
	}

	b.WriteString(")")

	fmt.Fprint(b, "\nRETURNS ", pgFunction.Returns)
	fmt.Fprint(b, "\nLANGUAGE ", pgFunction.Language)
	if pgFunction.Volatility != defaultFunctionVolatility {
		fmt.Fprint(b, "\n", pgFunction.Volatility)
	}
	if pgFunction.SecurityDefiner {
		fmt.Fprint(b, "\nSECURITY DEFINER")
	}
	if pgFunction.Parallel != defaultFunctionParallel {
		fmt.Fprint(b, "\nPARALLEL ", pgFunction.Parallel)
	}
	if pgFunction.Strict {
		fmt.Fprint(b, "\nSTRICT")
	}

	fmt.Fprint(b, "\nAS $function$", pgFunction.Body, "$function$;")

	sql := b.String()

	txn, err := startTransaction(db.client, d.Get(funcDatabaseAttr).(string))
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

func generateFunctionID(db *DBConnection, d *schema.ResourceData) (string, error) {

	b := bytes.NewBufferString("")

	if dbAttr, ok := d.GetOk(funcDatabaseAttr); ok {
		fmt.Fprint(b, dbAttr.(string), ".")
	} else {
		fmt.Fprint(b, db.client.databaseName, ".")
	}

	var pgFunction PGFunction
	err := pgFunction.FromResourceData(d)
	if err != nil {
		return "", err
	}

	fmt.Fprint(b, pgFunction.Schema, ".", pgFunction.Name, "(")

	argCount := 0

	for _, arg := range pgFunction.Args {
		mode := "IN"
		if arg.Mode != "" {
			mode = arg.Mode
		}

		if mode != "OUT" {
			if argCount > 0 {
				b.WriteRune(',')
			}

			b.WriteString(arg.Type)
			argCount += 1
		}
	}

	b.WriteRune(')')

	return b.String(), nil
}

func expandFunctionID(functionId string, d *schema.ResourceData, db *DBConnection) (databaseName string, functionSignature string, err error) {

	partsCount := strings.Count(functionId, ".") + 1

	if partsCount == 2 {
		clientDatabaseName := "postgres"
		if db != nil {
			clientDatabaseName = db.client.databaseName
		}

		signature, err := quoteSignature(functionId)
		if err != nil {
			return "", "", err
		}

		return getDatabase(d, clientDatabaseName), signature, nil
	}

	if partsCount == 3 {
		functionIdParts := strings.Split(functionId, ".")
		signature, err := quoteSignature(strings.Join(functionIdParts[1:], "."))
		if err != nil {
			return "", "", err
		}
		return functionIdParts[0], signature, nil
	}

	return "", "", fmt.Errorf("function ID %s has not the expected format 'database.schema.function_name(arguments)'", functionId)
}

func quoteSignature(s string) (signature string, err error) {

	signatureData := findStringSubmatchMap(`(?si)(?P<Schema>[^\.]+)\.(?P<Name>[^(]+)\((?P<Args>.*)\)`, s)

	schemaName, schemaFound := signatureData["Schema"]
	name, nameFound := signatureData["Name"]
	args, argsFound := signatureData["Args"]
	if schemaFound && nameFound && argsFound {
		return fmt.Sprintf("%s.%s(%s)", pq.QuoteIdentifier(schemaName), pq.QuoteIdentifier(name), args), nil
	}

	return "", fmt.Errorf("Incorrect signature format \"%s\". The expected format is schema.function_name(arguments)", s)
}
