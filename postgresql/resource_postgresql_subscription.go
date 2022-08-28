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
		},
	}
}

func resourcePostgreSQLSubscriptionCreate(db *DBConnection, d *schema.ResourceData) error {
	err := dbSupportsSubscription(db)
	if err != nil {
		return err
	}

	subName := d.Get("name").(string)
	databaseName := getDatabaseForSubscription(d, db.client.databaseName)

	publications, err := getPublicationsForSubscription(d)
	if err != nil {
		return fmt.Errorf("could not get publications: %w", err)
	}
	connInfo, err := getConnInfoForSubscription(d)

	optionalParams := getOptionalParameters(d)

	// Creating of a subscription can not be done in an transaction
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
	err := dbSupportsSubscription(db)
	if err != nil {
		return err
	}

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

	query := "SELECT subconninfo, subpublications, subslotname FROM pg_catalog.pg_subscription WHERE subname = $1"
	err = txn.QueryRow(query, pqQuoteLiteral(subName)).Scan(&connInfo, pq.Array(&publications), &slotName)

	switch {
	case err == sql.ErrNoRows:
		log.Printf("[WARN] PostgreSQL Subscription (%s) not found for database %s", subName, databaseName)
		d.SetId("")
		return nil
	case err != nil:
		return fmt.Errorf("Error reading subscription info: %w", err)
	}

	d.SetId(generateSubscriptionID(d, databaseName))
	d.Set("name", subName)
	d.Set(pubDatabaseAttr, databaseName)
	d.Set("conninfo", connInfo)
	d.Set("publications", publications)

	createSlot, okCreate := d.GetOkExists("create_slot")
	if okCreate {
		d.Set("create_slot", createSlot.(bool))
	}
	_, okSlotName := d.GetOk("slot_name")
	if okSlotName {
		d.Set("slot_name", slotName)
	}

	return nil
}

func resourcePostgreSQLSubscriptionDelete(db *DBConnection, d *schema.ResourceData) error {
	err := dbSupportsSubscription(db)
	if err != nil {
		return err
	}

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
	err := dbSupportsSubscription(db)
	if err != nil {
		return false, err
	}

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

	query := "SELECT subname FROM pg_catalog.pg_subscription WHERE subname = $1"
	err = txn.QueryRow(query, pqQuoteLiteral(subName)).Scan(&subName)

	switch {
	case err == sql.ErrNoRows:
		return false, nil
	case err != nil:
		return false, err
	}

	return true, nil
}

func dbSupportsSubscription(db *DBConnection) error {
	if !db.featureSupported(featureSubscription) {
		return fmt.Errorf(
			"postgresql_subscription resource is not supported for this Postgres version (%s)",
			db.version,
		)
	}
	return nil
}

func getPublicationsForSubscription(d *schema.ResourceData) (string, error) {
	var publicationsString string
	setPublications, ok := d.GetOk("publications")

	if !ok {
		return publicationsString, fmt.Errorf("Attribute publications is not set")
	}

	publications := setPublications.(*schema.Set).List()
	var plist []string
	if elem, ok := isUniqueArr(publications); !ok {
		return publicationsString, fmt.Errorf("'%s' is duplicated for attribute publications", elem.(string))
	}
	for _, p := range publications {
		plist = append(plist, fmt.Sprintf(pq.QuoteIdentifier(p.(string))))
	}

	return strings.Join(plist, ", "), nil
}

func getConnInfoForSubscription(d *schema.ResourceData) (string, error) {
	var connInfo string
	setConnInfo, ok := d.GetOk("conninfo")
	if !ok {
		return connInfo, fmt.Errorf("Attribute conninfo is not set")
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
			return "", "", fmt.Errorf("Subscription ID %s has not the expected format 'database.subscriptionName': %v", d.Id(), parsed)
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

	createSlot, okCreate := d.GetOkExists("create_slot")
	slotName, okName := d.GetOkExists("slot_name")

	if !okCreate && !okName {
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

	returnValue = fmt.Sprintf(parameterSQLTemplate, strings.Join(params, ", "))
	return returnValue
}

func getSubscriptionNameFromID(ID string) string {
	splitted := strings.Split(ID, ".")
	return splitted[0]
}
