package postgresql

import (
	"bytes"
	"database/sql"
	"fmt"
	"log"
	"strings"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/lib/pq"
)

const (
	procNameAttr            = "name"
	procSchemaAttr          = "schema"
	procBodyAttr            = "body"
	procArgAttr             = "arg"
	procLanguageAttr        = "language"
	procDropCascadeAttr     = "drop_cascade"
	procDatabaseAttr        = "database"
	procSecurityDefinerAttr = "security_definer"

	procArgTypeAttr    = "type"
	procArgNameAttr    = "name"
	procArgModeAttr    = "mode"
	procArgDefaultAttr = "default"
)

func resourcePostgreSQLProcedure() *schema.Resource {
	return &schema.Resource{
		Create: PGResourceFunc(resourcePostgreSQLProcedureCreate),
		Read:   PGResourceFunc(resourcePostgreSQLProcedureRead),
		Update: PGResourceFunc(resourcePostgreSQLProcedureUpdate),
		Delete: PGResourceFunc(resourcePostgreSQLProcedureDelete),
		Exists: PGResourceExistsFunc(resourcePostgreSQLProcedureExists),
		Importer: &schema.ResourceImporter{
			StateContext: schema.ImportStatePassthroughContext,
		},

		Schema: map[string]*schema.Schema{
			funcSchemaAttr: {
				Type:        schema.TypeString,
				Optional:    true,
				Computed:    true,
				ForceNew:    true,
				Description: "Schema where the procedure is located. If not specified, the provider default schema is used.",

				DiffSuppressFunc: defaultDiffSuppressFunc,
			},
			funcNameAttr: {
				Type:        schema.TypeString,
				Required:    true,
				ForceNew:    true,
				Description: "Name of the procedure.",
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
				Description: "Procedure argument definitions.",
			},
			funcLanguageAttr: {
				Type:        schema.TypeString,
				Optional:    true,
				ForceNew:    true,
				Default:     "plpgsql",
				Description: "Language of theof the procedure. One of: internal, sql, c, plpgsql",

				DiffSuppressFunc: defaultDiffSuppressFunc,
			},
			funcBodyAttr: {
				Type:        schema.TypeString,
				Required:    true,
				Description: "Body of the procedure.",

				DiffSuppressFunc: func(k, old, new string, d *schema.ResourceData) bool {
					return normalizeProcedureBody(new) == old
				},
				StateFunc: func(val interface{}) string {
					return normalizeProcedureBody(val.(string))
				},
			},
			funcDropCascadeAttr: {
				Type:        schema.TypeBool,
				Description: "Automatically drop objects that depend on the procedure (such as operators or triggers), and in turn all objects that depend on those objects.",
				Optional:    true,
				Default:     false,
			},
			funcSecurityDefinerAttr: {
				Type:        schema.TypeBool,
				Description: "If the procedure should execute with the permissions of the procedure owner instead of the permissions of the caller.",
				Optional:    true,
				Default:     false,
			},
			funcDatabaseAttr: {
				Type:        schema.TypeString,
				Optional:    true,
				Computed:    true,
				ForceNew:    true,
				Description: "The database where the procedure is located. If not specified, the provider default database is used.",

				DiffSuppressFunc: defaultDiffSuppressFunc,
			},
		},
	}
}

func resourcePostgreSQLProcedureCreate(db *DBConnection, d *schema.ResourceData) error {
	if !db.featureSupported(featureProcedure) {
		return fmt.Errorf(
			"postgresql_procedure resource is not supported for this Postgres version (%s)",
			db.version,
		)
	}

	if err := createProcedure(db, d, false); err != nil {
		return err
	}

	return resourcePostgreSQLProcedureReadImpl(db, d)
}

func resourcePostgreSQLProcedureExists(db *DBConnection, d *schema.ResourceData) (bool, error) {
	if !db.featureSupported(featureProcedure) {
		return false, fmt.Errorf(
			"postgresql_procedure resource is not supported for this Postgres version (%s)",
			db.version,
		)
	}

	procedureId := d.Id()

	databaseName, procedureSignature, expandErr := expandProcedureID(procedureId, d, db)
	if expandErr != nil {
		return false, expandErr
	}

	var procedureExists bool

	txn, err := startTransaction(db.client, databaseName)
	if err != nil {
		return false, err
	}
	defer deferredRollback(txn)

	query := fmt.Sprintf("SELECT to_regprocedure('%s') IS NOT NULL AS procedureExists", procedureSignature)

	if err := txn.QueryRow(query).Scan(&procedureExists); err != nil {
		return false, err
	}

	if err := txn.Commit(); err != nil {
		return false, err
	}

	return procedureExists, nil
}

func resourcePostgreSQLProcedureRead(db *DBConnection, d *schema.ResourceData) error {
	if !db.featureSupported(featureProcedure) {
		return fmt.Errorf(
			"postgresql_procedure resource is not supported for this Postgres version (%s)",
			db.version,
		)
	}

	return resourcePostgreSQLProcedureReadImpl(db, d)
}

func resourcePostgreSQLProcedureReadImpl(db *DBConnection, d *schema.ResourceData) error {
	procedureId := d.Id()

	if procedureId == "" {
		// Generate during creation
		generatedProcedureId, err := generateProcedureID(db, d)
		if err != nil {
			return err
		}
		procedureId = generatedProcedureId
	}

	databaseName, procedureSignature, expandErr := expandProcedureID(procedureId, d, db)
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

	err = txn.QueryRow(query, procedureSignature).Scan(&funcDefinition)
	switch {
	case err == sql.ErrNoRows:
		log.Printf("[WARN] PostgreSQL procedure: %s", procedureId)
		d.SetId("")
		return nil
	case err != nil:
		return fmt.Errorf("Error reading procedure: %w", err)
	}

	if err := txn.Commit(); err != nil {
		return err
	}

	var pgProcedure PGProcedure

	err = pgProcedure.Parse(funcDefinition)
	if err != nil {
		return err
	}

	var args []map[string]interface{}

	for _, a := range pgProcedure.Args {
		args = append(args, map[string]interface{}{
			funcArgTypeAttr:    a.Type,
			funcArgNameAttr:    a.Name,
			funcArgModeAttr:    a.Mode,
			funcArgDefaultAttr: a.Default,
		})
	}

	d.Set(funcDatabaseAttr, databaseName)
	d.Set(funcNameAttr, pgProcedure.Name)
	d.Set(funcSchemaAttr, pgProcedure.Schema)
	d.Set(funcLanguageAttr, pgProcedure.Language)
	d.Set(funcBodyAttr, pgProcedure.Body)
	d.Set(funcSecurityDefinerAttr, pgProcedure.SecurityDefiner)
	d.Set(funcArgAttr, args)

	d.SetId(procedureId)

	return nil
}

func resourcePostgreSQLProcedureDelete(db *DBConnection, d *schema.ResourceData) error {
	if !db.featureSupported(featureProcedure) {
		return fmt.Errorf(
			"postgresql_procedure resource is not supported for this Postgres version (%s)",
			db.version,
		)
	}

	databaseName, procedureSignature, err := expandProcedureID(d.Id(), d, db)
	if err != nil {
		return err
	}

	dropMode := "RESTRICT"
	if v, ok := d.GetOk(funcDropCascadeAttr); ok && v.(bool) {
		dropMode = "CASCADE"
	}

	sql := fmt.Sprintf("DROP PROCEDURE IF EXISTS %s %s", procedureSignature, dropMode)

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

func resourcePostgreSQLProcedureUpdate(db *DBConnection, d *schema.ResourceData) error {
	if !db.featureSupported(featureProcedure) {
		return fmt.Errorf(
			"postgresql_procedure resource is not supported for this Postgres version (%s)",
			db.version,
		)
	}

	if err := createProcedure(db, d, true); err != nil {
		return err
	}

	return resourcePostgreSQLProcedureReadImpl(db, d)
}

func createProcedure(db *DBConnection, d *schema.ResourceData, replace bool) error {

	var pgProcedure PGProcedure
	err := pgProcedure.FromResourceData(d)
	if err != nil {
		return err
	}

	b := bytes.NewBufferString("CREATE ")

	if replace {
		b.WriteString(" OR REPLACE ")
	}

	b.WriteString("PROCEDURE ")

	fmt.Fprint(b, pq.QuoteIdentifier(pgProcedure.Schema), ".")

	fmt.Fprint(b, pq.QuoteIdentifier(pgProcedure.Name), " (")

	for i, arg := range pgProcedure.Args {
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

	if len(pgProcedure.Args) > 0 {
		b.WriteRune('\n')
	}

	b.WriteString(")")

	fmt.Fprint(b, "\nLANGUAGE ", pgProcedure.Language)
	if pgProcedure.SecurityDefiner {
		fmt.Fprint(b, "\nSECURITY DEFINER")
	}

	fmt.Fprint(b, "\nAS $procedure$", pgProcedure.Body, "$procedure$;")

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

func generateProcedureID(db *DBConnection, d *schema.ResourceData) (string, error) {

	b := bytes.NewBufferString("")

	if dbAttr, ok := d.GetOk(funcDatabaseAttr); ok {
		fmt.Fprint(b, dbAttr.(string), ".")
	} else {
		fmt.Fprint(b, db.client.databaseName, ".")
	}

	var pgProcedure PGProcedure
	err := pgProcedure.FromResourceData(d)
	if err != nil {
		return "", err
	}

	fmt.Fprint(b, pgProcedure.Schema, ".", pgProcedure.Name, "(")

	argCount := 0

	for _, arg := range pgProcedure.Args {
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

func expandProcedureID(procedureId string, d *schema.ResourceData, db *DBConnection) (databaseName string, procedureSignature string, err error) {
	partsCount := strings.Count(procedureId, ".") + 1

	if partsCount == 2 {
		clientDatabaseName := "postgres"
		if db != nil {
			clientDatabaseName = db.client.databaseName
		}

		signature, err := quoteSignature(procedureId)
		if err != nil {
			return "", "", err
		}

		return getDatabase(d, clientDatabaseName), signature, nil
	}

	if partsCount == 3 {
		procedureIdParts := strings.Split(procedureId, ".")
		signature, err := quoteSignature(strings.Join(procedureIdParts[1:], "."))
		if err != nil {
			return "", "", err
		}
		return procedureIdParts[0], signature, nil
	}

	return "", "", fmt.Errorf("procedure ID %s has not the expected format 'database.schema.procedure_name(arguments)'", procedureId)
}
