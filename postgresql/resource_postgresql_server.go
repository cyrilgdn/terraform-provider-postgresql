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
	serverNameAttr        = "server_name"
	serverTypeAttr        = "server_type"
	serverVersionAttr     = "server_version"
	serverOwnerAttr       = "server_owner"
	serverFDWAttr         = "fdw_name"
	serverOptionsAttr     = "options"
	serverDropCascadeAttr = "drop_cascade"
)

func resourcePostgreSQLServer() *schema.Resource {
	return &schema.Resource{
		Create: PGResourceFunc(resourcePostgreSQLServerCreate),
		Read:   PGResourceFunc(resourcePostgreSQLServerRead),
		Update: PGResourceFunc(resourcePostgreSQLServerUpdate),
		Delete: PGResourceFunc(resourcePostgreSQLServerDelete),
		Exists: PGResourceExistsFunc(resourcePostgreSQLServerExists),
		Importer: &schema.ResourceImporter{
			StateContext: schema.ImportStatePassthroughContext,
		},

		Schema: map[string]*schema.Schema{
			serverNameAttr: {
				Type:        schema.TypeString,
				Required:    true,
				Description: "The name of the foreign server to be created",
			},
			serverTypeAttr: {
				Type:        schema.TypeString,
				Optional:    true,
				ForceNew:    true,
				Description: "Optional server type, potentially useful to foreign-data wrappers",
			},
			serverVersionAttr: {
				Type:        schema.TypeString,
				Optional:    true,
				Description: "Optional server version, potentially useful to foreign-data wrappers.",
			},
			serverFDWAttr: {
				Type:        schema.TypeString,
				Required:    true,
				ForceNew:    true,
				Description: "The name of the foreign-data wrapper that manages the server",
			},
			serverOwnerAttr: {
				Type:        schema.TypeString,
				Optional:    true,
				Computed:    true,
				Description: "The user name of the new owner of the foreign server",
			},
			serverOptionsAttr: {
				Type: schema.TypeMap,
				Elem: &schema.Schema{
					Type: schema.TypeString,
				},
				Optional:    true,
				Description: "This clause specifies the options for the server. The options typically define the connection details of the server, but the actual names and values are dependent on the server's foreign-data wrapper",
			},
			serverDropCascadeAttr: {
				Type:        schema.TypeBool,
				Optional:    true,
				Default:     false,
				Description: "Automatically drop objects that depend on the server (such as user mappings), and in turn all objects that depend on those objects. Drop RESTRICT is the default",
			},
		},
	}
}

func resourcePostgreSQLServerCreate(db *DBConnection, d *schema.ResourceData) error {
	if !db.featureSupported(featureServer) {
		return fmt.Errorf(
			"Foreign Server resource is not supported for this Postgres version (%s)",
			db.version,
		)
	}

	serverName := d.Get(serverNameAttr).(string)

	b := bytes.NewBufferString("CREATE SERVER ")
	fmt.Fprint(b, pq.QuoteIdentifier(serverName))

	if v, ok := d.GetOk(serverTypeAttr); ok {
		fmt.Fprint(b, " TYPE ", pq.QuoteLiteral(v.(string)))
	}

	if v, ok := d.GetOk(serverVersionAttr); ok {
		fmt.Fprint(b, " VERSION ", pq.QuoteLiteral(v.(string)))
	}

	fmt.Fprint(b, " FOREIGN DATA WRAPPER ", pq.QuoteIdentifier(d.Get(serverFDWAttr).(string)))

	if options, ok := d.GetOk(serverOptionsAttr); ok {
		fmt.Fprint(b, " OPTIONS ( ")
		cnt := 0
		len := len(options.(map[string]interface{}))
		for k, v := range options.(map[string]interface{}) {
			fmt.Fprint(b, " ", pq.QuoteIdentifier(k), " ", pq.QuoteLiteral(v.(string)))
			if cnt < len-1 {
				fmt.Fprint(b, ", ")
			}
			cnt++
		}
		fmt.Fprint(b, " ) ")
	}

	txn, err := startTransaction(db.client, "")
	if err != nil {
		return err
	}
	defer deferredRollback(txn)

	sql := b.String()
	if _, err := txn.Exec(sql); err != nil {
		return err
	}

	if v, ok := d.GetOk(serverOwnerAttr); ok {
		currentUser, err := getCurrentUser(txn)
		if err != nil {
			return err
		}
		if v != currentUser {
			if err := setServerOwner(txn, d); err != nil {
				return err
			}
		}
	}

	if err = txn.Commit(); err != nil {
		return fmt.Errorf("Error creating server: %w", err)
	}

	d.SetId(d.Get(serverNameAttr).(string))

	return resourcePostgreSQLServerReadImpl(db, d)
}

func resourcePostgreSQLServerExists(db *DBConnection, d *schema.ResourceData) (bool, error) {
	if !db.featureSupported(featureServer) {
		return false, fmt.Errorf(
			"Foreign Server resource is not supported for this Postgres version (%s)",
			db.version,
		)
	}

	serverName := d.Get(serverNameAttr).(string)

	// Check if the database exists
	exists, err := foreignServerExists(db, serverName)
	if err != nil || !exists {
		return false, err
	}

	txn, err := startTransaction(db.client, "")
	if err != nil {
		return false, err
	}
	defer deferredRollback(txn)

	query := "SELECT srvname FROM pg_foreign_server WHERE srvname = $1"
	err = txn.QueryRow(query, serverName).Scan(&serverName)
	switch {
	case err == sql.ErrNoRows:
		return false, nil
	case err != nil:
		return false, err
	}

	return true, nil
}

func resourcePostgreSQLServerRead(db *DBConnection, d *schema.ResourceData) error {
	if !db.featureSupported(featureServer) {
		return fmt.Errorf(
			"Foreign Server resource is not supported for this Postgres version (%s)",
			db.version,
		)
	}

	return resourcePostgreSQLServerReadImpl(db, d)
}

func resourcePostgreSQLServerReadImpl(db *DBConnection, d *schema.ResourceData) error {
	serverName := d.Get(serverNameAttr).(string)
	txn, err := startTransaction(db.client, "")
	if err != nil {
		return err
	}
	defer deferredRollback(txn)

	var serverType, serverVersion, serverOwner, serverFDW string
	var serverOptions []string
	query := `SELECT COALESCE(fs.srvtype, ''), COALESCE(fs.srvversion, ''), fs.srvowner::regrole, fs.srvoptions, w.fdwname ` +
		`FROM pg_foreign_server fs JOIN pg_foreign_data_wrapper w on w.oid = fs.srvfdw ` +
		`WHERE fs.srvname = $1`
	err = txn.QueryRow(query, serverName).Scan(&serverType, &serverVersion, &serverOwner, pq.Array(&serverOptions), &serverFDW)
	switch {
	case err == sql.ErrNoRows:
		log.Printf("[WARN] PostgreSQL foreign server (%s) not found", serverName)
		d.SetId("")
		return nil
	case err != nil:
		return fmt.Errorf("Error reading foreign server: %w", err)
	}

	mappedOptions := make(map[string]interface{})
	for _, v := range serverOptions {
		pair := strings.Split(v, "=")
		mappedOptions[pair[0]] = pair[1]
	}

	d.Set(serverNameAttr, serverName)
	d.Set(serverTypeAttr, serverType)
	d.Set(serverVersionAttr, serverVersion)
	d.Set(serverOwnerAttr, serverOwner)
	d.Set(serverOptionsAttr, mappedOptions)
	d.Set(serverFDWAttr, serverFDW)
	d.SetId(serverName)

	return nil
}

func resourcePostgreSQLServerDelete(db *DBConnection, d *schema.ResourceData) error {
	if !db.featureSupported(featureServer) {
		return fmt.Errorf(
			"Foreign Server resource is not supported for this Postgres version (%s)",
			db.version,
		)
	}

	serverName := d.Get(serverNameAttr).(string)

	txn, err := startTransaction(db.client, "")
	if err != nil {
		return err
	}
	defer deferredRollback(txn)

	dropMode := "RESTRICT"
	if d.Get(serverDropCascadeAttr).(bool) {
		dropMode = "CASCADE"
	}

	sql := fmt.Sprintf("DROP SERVER %s %s ", pq.QuoteIdentifier(serverName), dropMode)
	if _, err := txn.Exec(sql); err != nil {
		return err
	}

	if err = txn.Commit(); err != nil {
		return fmt.Errorf("Error deleting server: %w", err)
	}

	d.SetId("")

	return nil
}

func resourcePostgreSQLServerUpdate(db *DBConnection, d *schema.ResourceData) error {
	if !db.featureSupported(featureServer) {
		return fmt.Errorf(
			"Foreign Server resource is not supported for this Postgres version (%s)",
			db.version,
		)
	}

	txn, err := startTransaction(db.client, "")
	if err != nil {
		return err
	}
	defer deferredRollback(txn)

	if err := setServerNameIfChanged(txn, d); err != nil {
		return err
	}

	if err := setServerOwnerIfChanged(txn, d); err != nil {
		return err
	}

	if err := setServerVersionOptionsIfChanged(txn, d); err != nil {
		return err
	}

	if err = txn.Commit(); err != nil {
		return fmt.Errorf("Error updating foreign server: %w", err)
	}

	return resourcePostgreSQLServerReadImpl(db, d)
}

func setServerVersionOptionsIfChanged(txn *sql.Tx, d *schema.ResourceData) error {
	if !d.HasChange(serverVersionAttr) && !d.HasChange(serverOptionsAttr) {
		return nil
	}

	b := bytes.NewBufferString("ALTER SERVER ")
	serverName := d.Get(serverNameAttr).(string)

	fmt.Fprintf(b, "%s ", pq.QuoteIdentifier(serverName))

	if d.HasChange(serverVersionAttr) {
		fmt.Fprintf(b, "VERSION %s", pq.QuoteLiteral(d.Get(serverVersionAttr).(string)))
	}

	if d.HasChange(serverOptionsAttr) {
		oldOptions, newOptions := d.GetChange(serverOptionsAttr)
		fmt.Fprint(b, " OPTIONS ( ")
		cnt := 0
		len := len(newOptions.(map[string]interface{}))
		toRemove := oldOptions.(map[string]interface{})
		for k, v := range newOptions.(map[string]interface{}) {
			operation := "ADD"
			if oldOptions.(map[string]interface{})[k] != nil {
				operation = "SET"
				delete(toRemove, k)
			}
			fmt.Fprint(b, " ", operation, " ", pq.QuoteIdentifier(k), " ", pq.QuoteLiteral(v.(string)))
			if cnt < len-1 {
				fmt.Fprint(b, ", ")
			}
			cnt++
		}

		for k := range toRemove {
			if cnt != 0 { // starting with 0 means to drop all the options. Cannot start with comma
				fmt.Fprint(b, " , ")
			}
			fmt.Fprint(b, " DROP ", pq.QuoteIdentifier(k))
			cnt++
		}

		fmt.Fprint(b, " ) ")
	}

	sql := b.String()
	if _, err := txn.Exec(sql); err != nil {
		return fmt.Errorf("Error updating foreign server version and/or options: %w", err)
	}

	return nil
}

func setServerNameIfChanged(txn *sql.Tx, d *schema.ResourceData) error {
	if !d.HasChange(serverNameAttr) {
		return nil
	}

	serverOldName, serverNewName := d.GetChange(serverNameAttr)

	b := bytes.NewBufferString("ALTER SERVER ")
	fmt.Fprintf(b, "%s RENAME TO %s", pq.QuoteIdentifier(serverOldName.(string)), pq.QuoteIdentifier(serverNewName.(string)))

	sql := b.String()
	if _, err := txn.Exec(sql); err != nil {
		return fmt.Errorf("Error updating foreign server name: %w", err)
	}

	return nil
}

func setServerOwnerIfChanged(txn *sql.Tx, d *schema.ResourceData) error {
	if !d.HasChange(serverOwnerAttr) {
		return nil
	}
	return setServerOwner(txn, d)
}

func setServerOwner(txn *sql.Tx, d *schema.ResourceData) error {
	serverName := d.Get(serverNameAttr).(string)
	serverNewOwner := d.Get(serverOwnerAttr).(string)

	b := bytes.NewBufferString("ALTER SERVER ")
	fmt.Fprintf(b, "%s OWNER TO %s", pq.QuoteIdentifier(serverName), pq.QuoteIdentifier(serverNewOwner))

	sql := b.String()
	if _, err := txn.Exec(sql); err != nil {
		return fmt.Errorf("Error updating foreign server owner: %w", err)
	}

	return nil
}
