package postgresql

import (
	"database/sql"
	"fmt"
	"log"
	"strings"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/validation"
	"github.com/lib/pq"
)

func resourcePostgreSQLSubscription() *schema.Resource {
	return &schema.Resource{
		Create:   PGResourceFunc(resourcePostgreSQLSubscriptionCreate),
		Read:     PGResourceFunc(resourcePostgreSQLSubscriptionRead),
		Update:   PGResourceFunc(resourcePostgreSQLSubscriptionUpdate),
		Delete:   PGResourceFunc(resourcePostgreSQLSubscriptionDelete),
		Exists:   PGResourceExistsFunc(resourcePostgreSQLSubscriptionExists),
		Importer: &schema.ResourceImporter{StateContext: schema.ImportStatePassthroughContext},

		Schema: map[string]*schema.Schema{
			"name": {
				Type:         schema.TypeString,
				Required:     true,
				ForceNew:     true,
				Description:  "The name of the subscription",
				ValidateFunc: validation.StringIsNotEmpty,
			},
			"database": {
				Type:        schema.TypeString,
				Optional:    true,
				Computed:    true,
				ForceNew:    true,
				Description: "Sets the database to add the subscription for",
			},
			"conninfo": {
				Type:         schema.TypeString,
				Required:     true,
				ForceNew:     true,
				Sensitive:    true,
				Description:  "The connection string to the publisher. It should follow the keyword/value format (https://www.postgresql.org/docs/current/libpq-connect.html#LIBPQ-CONNSTRING)",
				ValidateFunc: validation.StringIsNotEmpty,
			},
			"publications": {
				Type:        schema.TypeSet,
				Required:    true,
				ForceNew:    true,
				Elem:        &schema.Schema{Type: schema.TypeString},
				Description: "Names of the publications on the publisher to subscribe to",
			},
			"create_slot": {
				Type:        schema.TypeBool,
				Optional:    true,
				ForceNew:    true,
				Default:     true,
				Description: "Specifies whether the command should create the replication slot on the publisher",
			},
			"slot_name": {
				Type:         schema.TypeString,
				Optional:     true,
				ForceNew:     true,
				Description:  "Name of the replication slot to use. The default behavior is to use the name of the subscription for the slot name",
				ValidateFunc: validation.StringIsNotEmpty,
			},
			"enabled": {
				Type:        schema.TypeBool,
				Optional:    true,
				Default:     true,
				Description: "Specifies whether the subscription should be actively replicating or whether it should just be set up but not started yet. The default is true.",
			},
		},
	}
}

func resourcePostgreSQLSubscriptionCreate(db *DBConnection, d *schema.ResourceData) error {
	subName := d.Get("name").(string)
	databaseName := getDatabaseForSubscription(d, db.client.databaseName)

	publications, err := getPublicationsForSubscription(d)
	if err != nil {
		return fmt.Errorf("could not get publications: %w", err)
	}
	connInfo, err := getConnInfoForSubscription(d)
	if err != nil {
		return fmt.Errorf("could not get conninfo: %w", err)
	}

	optionalParams := getOptionalParameters(d)

	// Creating of a subscription can not be done in a transaction
	client := db.client.config.NewClient(databaseName)
	conn, err := client.Connect()
	if err != nil {
		return fmt.Errorf("could not establish database connection: %w", err)
	}

	sql := fmt.Sprintf("CREATE SUBSCRIPTION %s CONNECTION %s PUBLICATION %s %s;",
		pq.QuoteIdentifier(subName),
		pq.QuoteLiteral(connInfo),
		publications,
		optionalParams,
	)
	if _, err := conn.Exec(sql); err != nil {
		return fmt.Errorf("could not execute sql: %w", err)
	}

	d.SetId(generateSubscriptionID(d, databaseName))

	return resourcePostgreSQLSubscriptionReadImpl(db, d)
}

func resourcePostgreSQLSubscriptionRead(db *DBConnection, d *schema.ResourceData) error {
	return resourcePostgreSQLSubscriptionReadImpl(db, d)
}

func resourcePostgreSQLSubscriptionReadImpl(db *DBConnection, d *schema.ResourceData) error {
	databaseName, subName, err := getDBSubscriptionName(d, db.client)
	if err != nil {
		return fmt.Errorf("could not get subscription name: %w", err)
	}

	txn, err := startTransaction(db.client, databaseName)
	if err != nil {
		return fmt.Errorf("could not start transaction: %w", err)
	}
	defer deferredRollback(txn)

	var publications []string
	var connInfo string
	var slotName string
	var enabled bool

	var subExists bool
	queryExists := "SELECT TRUE FROM pg_catalog.pg_stat_subscription WHERE subname = $1"
	err = txn.QueryRow(queryExists, pqQuoteLiteral(subName)).Scan(&subExists)
	if err != nil {
		return fmt.Errorf("failed to check subscription: %w", err)
	}

	if !subExists {
		log.Printf("[WARN] PostgreSQL Subscription (%s) not found for database %s", subName, databaseName)
		d.SetId("")
		return nil
	}

	// pg_subscription requires superuser permissions, it is okay to fail here
	query := "SELECT subconninfo, subpublications, subslotname, subenabled FROM pg_catalog.pg_subscription WHERE subname = $1"
	err = txn.QueryRow(query, pqQuoteLiteral(subName)).Scan(&connInfo, pq.Array(&publications), &slotName, &enabled)

	if err != nil {
		// we already checked that the subscription exists
		connInfo, err := getConnInfoForSubscription(d)
		if err != nil {
			return fmt.Errorf("could not get conninfo: %w", err)
		}
		d.Set("conninfo", connInfo)

		setPublications, ok := d.GetOk("publications")
		if !ok {
			return fmt.Errorf("attribute publications is not set")
		}
		publications := setPublications.(*schema.Set).List()
		d.Set("publications", publications)
		
		// Set enabled from config since we can't read it from the database
		enabled := d.Get("enabled").(bool)
		d.Set("enabled", enabled)
	} else {
		d.Set("conninfo", connInfo)
		d.Set("publications", publications)
		d.Set("enabled", enabled)
	}
	d.Set("name", subName)
	d.Set("database", databaseName)
	d.SetId(generateSubscriptionID(d, databaseName))

	createSlot, okCreate := d.GetOkExists("create_slot") //nolint:staticcheck
	if okCreate {
		d.Set("create_slot", createSlot.(bool))
	}
	_, okSlotName := d.GetOk("slot_name")
	if okSlotName {
		d.Set("slot_name", slotName)
	}

	return nil
}

func resourcePostgreSQLSubscriptionUpdate(db *DBConnection, d *schema.ResourceData) error {
	subName := d.Get("name").(string)
	databaseName := getDatabaseForSubscription(d, db.client.databaseName)

	// Check if enabled has changed
	if d.HasChange("enabled") {
		enabled := d.Get("enabled").(bool)

		// Subscription operations cannot be done in a transaction
		client := db.client.config.NewClient(databaseName)
		conn, err := client.Connect()
		if err != nil {
			return fmt.Errorf("could not establish database connection: %w", err)
		}

		var sql string
		if enabled {
			sql = fmt.Sprintf("ALTER SUBSCRIPTION %s ENABLE", pq.QuoteIdentifier(subName))
		} else {
			sql = fmt.Sprintf("ALTER SUBSCRIPTION %s DISABLE", pq.QuoteIdentifier(subName))
		}

		if _, err := conn.Exec(sql); err != nil {
			return fmt.Errorf("could not execute sql: %w", err)
		}
	}

	return resourcePostgreSQLSubscriptionReadImpl(db, d)
}

func resourcePostgreSQLSubscriptionDelete(db *DBConnection, d *schema.ResourceData) error {
	subName := d.Get("name").(string)
	createSlot := d.Get("create_slot").(bool)

	databaseName := getDatabaseForSubscription(d, db.client.databaseName)

	// Dropping a subscription can not be done in a transaction
	client := db.client.config.NewClient(databaseName)
	conn, err := client.Connect()
	if err != nil {
		return fmt.Errorf("could not establish database connection: %w", err)
	}

	// disable subscription and unset the slot before dropping in order to keep the replication slot
	if !createSlot {
		sql := fmt.Sprintf("ALTER SUBSCRIPTION %s DISABLE", pq.QuoteIdentifier(subName))
		if _, err := conn.Exec(sql); err != nil {
			return fmt.Errorf("could not execute sql: %w", err)
		}
		sql = fmt.Sprintf("ALTER SUBSCRIPTION %s SET (slot_name = NONE)", pq.QuoteIdentifier(subName))
		if _, err := conn.Exec(sql); err != nil {
			return fmt.Errorf("could not execute sql: %w", err)
		}
	}

	sql := fmt.Sprintf("DROP SUBSCRIPTION %s", pq.QuoteIdentifier(subName))

	if _, err := conn.Exec(sql); err != nil {
		return fmt.Errorf("could not execute sql: %w", err)
	}

	d.SetId("")

	return nil
}

func resourcePostgreSQLSubscriptionExists(db *DBConnection, d *schema.ResourceData) (bool, error) {
	var subName string

	database, subName, err := getDBSubscriptionName(d, db.client)
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

	query := "SELECT subname from pg_catalog.pg_stat_subscription WHERE subname = $1"
	err = txn.QueryRow(query, pqQuoteLiteral(subName)).Scan(&subName)

	switch {
	case err == sql.ErrNoRows:
		return false, nil
	case err != nil:
		return false, err
	}

	return true, nil
}

func getPublicationsForSubscription(d *schema.ResourceData) (string, error) {
	var publicationsString string
	setPublications, ok := d.GetOk("publications")

	if !ok {
		return publicationsString, fmt.Errorf("attribute publications is not set")
	}

	publications := setPublications.(*schema.Set).List()
	var plist []string
	if elem, ok := isUniqueArr(publications); !ok {
		return publicationsString, fmt.Errorf("'%s' is duplicated for attribute publications", elem.(string))
	}
	for _, p := range publications {
		plist = append(plist, pq.QuoteIdentifier(p.(string)))
	}

	return strings.Join(plist, ", "), nil
}

func getConnInfoForSubscription(d *schema.ResourceData) (string, error) {
	var connInfo string
	setConnInfo, ok := d.GetOk("conninfo")
	if !ok {
		return connInfo, fmt.Errorf("attribute conninfo is not set")
	}
	return setConnInfo.(string), nil
}

func generateSubscriptionID(d *schema.ResourceData, databaseName string) string {
	return strings.Join([]string{databaseName, d.Get("name").(string)}, ".")
}

func getDatabaseForSubscription(d *schema.ResourceData, databaseName string) string {
	if v, ok := d.GetOk("database"); ok {
		databaseName = v.(string)
	}

	return databaseName
}

// getDBSubscriptionName returns database and subscription name. If we are importing this
// resource, they will be parsed from the resource ID (it will return an error if parsing failed)
// otherwise they will be simply get from the state.
func getDBSubscriptionName(d *schema.ResourceData, client *Client) (string, string, error) {
	database := getDatabaseForSubscription(d, client.databaseName)
	subName := d.Get("name").(string)

	// When importing, we have to parse the ID to find subscription and database names.
	if subName == "" {
		parsed := strings.Split(d.Id(), ".")
		if len(parsed) != 2 {
			return "", "", fmt.Errorf("subscription ID %s has not the expected format 'database.subscriptionName': %v", d.Id(), parsed)
		}
		database = parsed[0]
		subName = parsed[1]
	}

	return database, subName, nil
}

// slotName and createSlot require recreation of the subscription, only return WITH ...
func getOptionalParameters(d *schema.ResourceData) string {
	parameterSQLTemplate := "WITH (%s)"
	returnValue := ""

	createSlot, okCreate := d.GetOkExists("create_slot") //nolint:staticcheck
	slotName, okName := d.GetOk("slot_name")
	enabled, okEnabled := d.GetOkExists("enabled") //nolint:staticcheck

	if !okCreate && !okName && !okEnabled {
		// use default behavior, no WITH statement
		return ""
	}

	var params []string
	if okCreate {
		params = append(params, fmt.Sprintf("%s = %t", "create_slot", createSlot.(bool)))
	}
	if okName {
		params = append(params, fmt.Sprintf("%s = %s", "slot_name", pq.QuoteLiteral(slotName.(string))))
	}
	if okEnabled {
		params = append(params, fmt.Sprintf("%s = %t", "enabled", enabled.(bool)))
	}

	returnValue = fmt.Sprintf(parameterSQLTemplate, strings.Join(params, ", "))
	return returnValue
}

func getSubscriptionNameFromID(ID string) string {
	splitted := strings.Split(ID, ".")
	return splitted[0]
}
