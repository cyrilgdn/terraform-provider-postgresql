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
	userMappingUserNameAttr   = "user_name"
	userMappingServerNameAttr = "server_name"
	userMappingOptionsAttr    = "options"
)

func resourcePostgreSQLUserMapping() *schema.Resource {
	return &schema.Resource{
		Create: PGResourceFunc(resourcePostgreSQLUserMappingCreate),
		Read:   PGResourceFunc(resourcePostgreSQLUserMappingRead),
		Update: PGResourceFunc(resourcePostgreSQLUserMappingUpdate),
		Delete: PGResourceFunc(resourcePostgreSQLUserMappingDelete),
		Importer: &schema.ResourceImporter{
			StateContext: schema.ImportStatePassthroughContext,
		},

		Schema: map[string]*schema.Schema{
			userMappingUserNameAttr: {
				Type:        schema.TypeString,
				Required:    true,
				ForceNew:    true,
				Description: "The name of an existing user that is mapped to foreign server. CURRENT_ROLE, CURRENT_USER, and USER match the name of the current user. When PUBLIC is specified, a so-called public mapping is created that is used when no user-specific mapping is applicable",
			},
			userMappingServerNameAttr: {
				Type:        schema.TypeString,
				Required:    true,
				ForceNew:    true,
				Description: "The name of an existing server for which the user mapping is to be created",
			},
			userMappingOptionsAttr: {
				Type: schema.TypeMap,
				Elem: &schema.Schema{
					Type: schema.TypeString,
				},
				Optional:    true,
				Description: "This clause specifies the options of the user mapping. The options typically define the actual user name and password of the mapping. Option names must be unique. The allowed option names and values are specific to the server's foreign-data wrapper",
			},
		},
	}
}

func resourcePostgreSQLUserMappingCreate(db *DBConnection, d *schema.ResourceData) error {
	if !db.featureSupported(featureServer) {
		return fmt.Errorf(
			"foreign server resource is not supported for this Postgres version (%s)",
			db.version,
		)
	}

	username := d.Get(userMappingUserNameAttr).(string)
	serverName := d.Get(userMappingServerNameAttr).(string)

	b := bytes.NewBufferString("CREATE USER MAPPING ")
	fmt.Fprint(b, " FOR ", pq.QuoteIdentifier(username))
	fmt.Fprint(b, " SERVER ", pq.QuoteIdentifier(serverName))

	if options, ok := d.GetOk(userMappingOptionsAttr); ok {
		fmt.Fprint(b, " OPTIONS ( ")
		cnt := 0
		len := len(options.(map[string]any))
		for k, v := range options.(map[string]any) {
			fmt.Fprint(b, " ", pq.QuoteIdentifier(k), " ", pq.QuoteLiteral(v.(string)))
			if cnt < len-1 {
				fmt.Fprint(b, ", ")
			}
			cnt++
		}
		fmt.Fprint(b, " ) ")
	}

	if _, err := db.Exec(b.String()); err != nil {
		return fmt.Errorf("could not create user mapping: %w", err)
	}

	d.SetId(generateUserMappingID(d))

	return resourcePostgreSQLUserMappingReadImpl(db, d)
}

func resourcePostgreSQLUserMappingRead(db *DBConnection, d *schema.ResourceData) error {
	if !db.featureSupported(featureServer) {
		return fmt.Errorf(
			"foreign server resource is not supported for this Postgres version (%s)",
			db.version,
		)
	}

	return resourcePostgreSQLUserMappingReadImpl(db, d)
}

func resourcePostgreSQLUserMappingReadImpl(db *DBConnection, d *schema.ResourceData) error {
	username := d.Get(userMappingUserNameAttr).(string)
	serverName := d.Get(userMappingServerNameAttr).(string)

	txn, err := startTransaction(db.client, "")
	if err != nil {
		return err
	}
	defer deferredRollback(txn)

	var userMappingOptions []string
	query := "SELECT umoptions FROM information_schema._pg_user_mappings WHERE authorization_identifier = $1 and foreign_server_name = $2"
	err = txn.QueryRow(query, username, serverName).Scan(pq.Array(&userMappingOptions))

	if err != sql.ErrNoRows && err != nil {
		// Fallback to pg_user_mappings table if information_schema._pg_user_mappings is not available
		query := "SELECT umoptions FROM pg_user_mappings WHERE usename = $1 and srvname = $2"
		err = txn.QueryRow(query, username, serverName).Scan(pq.Array(&userMappingOptions))
	}

	switch {
	case err == sql.ErrNoRows:
		log.Printf("[WARN] PostgreSQL user mapping (%s) for server (%s) not found", username, serverName)
		d.SetId("")
		return nil
	case err != nil:
		return fmt.Errorf("error reading user mapping: %w", err)
	}

	mappedOptions := make(map[string]any)
	for _, v := range userMappingOptions {
		pair := strings.SplitN(v, "=", 2)
		mappedOptions[pair[0]] = pair[1]
	}

	d.Set(userMappingUserNameAttr, username)
	d.Set(userMappingServerNameAttr, serverName)
	d.Set(userMappingOptionsAttr, mappedOptions)
	d.SetId(generateUserMappingID(d))

	return nil
}

func resourcePostgreSQLUserMappingDelete(db *DBConnection, d *schema.ResourceData) error {
	if !db.featureSupported(featureServer) {
		return fmt.Errorf(
			"foreign server resource is not supported for this Postgres version (%s)",
			db.version,
		)
	}

	username := d.Get(userMappingUserNameAttr).(string)
	serverName := d.Get(userMappingServerNameAttr).(string)

	txn, err := startTransaction(db.client, "")
	if err != nil {
		return err
	}
	defer deferredRollback(txn)

	sql := fmt.Sprintf("DROP USER MAPPING FOR %s SERVER %s ", pq.QuoteIdentifier(username), pq.QuoteIdentifier(serverName))
	if _, err := txn.Exec(sql); err != nil {
		return err
	}

	if err = txn.Commit(); err != nil {
		return fmt.Errorf("error deleting user mapping: %w", err)
	}

	d.SetId("")

	return nil
}

func resourcePostgreSQLUserMappingUpdate(db *DBConnection, d *schema.ResourceData) error {
	if !db.featureSupported(featureServer) {
		return fmt.Errorf(
			"foreign server resource is not supported for this Postgres version (%s)",
			db.version,
		)
	}

	if err := setUserMappingOptionsIfChanged(db, d); err != nil {
		return err
	}

	return resourcePostgreSQLUserMappingReadImpl(db, d)
}

func setUserMappingOptionsIfChanged(db *DBConnection, d *schema.ResourceData) error {
	if !d.HasChange(userMappingOptionsAttr) {
		return nil
	}

	username := d.Get(userMappingUserNameAttr).(string)
	serverName := d.Get(userMappingServerNameAttr).(string)

	b := bytes.NewBufferString("ALTER USER MAPPING ")
	fmt.Fprintf(b, " FOR %s SERVER %s ", pq.QuoteIdentifier(username), pq.QuoteIdentifier(serverName))

	oldOptions, newOptions := d.GetChange(userMappingOptionsAttr)
	fmt.Fprint(b, " OPTIONS ( ")
	cnt := 0
	len := len(newOptions.(map[string]any))
	toRemove := oldOptions.(map[string]any)
	for k, v := range newOptions.(map[string]any) {
		operation := "ADD"
		if oldOptions.(map[string]any)[k] != nil {
			operation = "SET"
			delete(toRemove, k)
		}
		fmt.Fprintf(b, " %s %s %s ", operation, pq.QuoteIdentifier(k), pq.QuoteLiteral(v.(string)))
		if cnt < len-1 {
			fmt.Fprint(b, ", ")
		}
		cnt++
	}

	for k := range toRemove {
		if cnt != 0 { // starting with 0 means to drop all the options. Cannot start with comma
			fmt.Fprint(b, " , ")
		}
		fmt.Fprintf(b, " DROP %s ", pq.QuoteIdentifier(k))
		cnt++
	}

	fmt.Fprint(b, " ) ")

	if _, err := db.Exec(b.String()); err != nil {
		return fmt.Errorf("error updating user mapping options: %w", err)
	}

	return nil
}

func generateUserMappingID(d *schema.ResourceData) string {
	return strings.Join([]string{
		d.Get(userMappingUserNameAttr).(string),
		d.Get(userMappingServerNameAttr).(string),
	}, ".")
}
