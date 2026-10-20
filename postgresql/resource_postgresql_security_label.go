package postgresql

import (
	"bytes"
	"database/sql"
	"fmt"
	"log"
	"regexp"
	"strings"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/lib/pq"
)

const (
	securityLabelObjectNameAttr = "object_name"
	securityLabelObjectTypeAttr = "object_type"
	securityLabelProviderAttr   = "label_provider"
	securityLabelLabelAttr      = "label"
)

func resourcePostgreSQLSecurityLabel() *schema.Resource {
	return &schema.Resource{
		Create: PGResourceFunc(resourcePostgreSQLSecurityLabelCreate),
		Read:   PGResourceFunc(resourcePostgreSQLSecurityLabelRead),
		Update: PGResourceFunc(resourcePostgreSQLSecurityLabelUpdate),
		Delete: PGResourceFunc(resourcePostgreSQLSecurityLabelDelete),
		Importer: &schema.ResourceImporter{
			StateContext: schema.ImportStatePassthroughContext,
		},

		Schema: map[string]*schema.Schema{
			securityLabelObjectNameAttr: {
				Type:        schema.TypeString,
				Required:    true,
				ForceNew:    true,
				Description: "The name of the existing object to apply the security label to",
			},
			securityLabelObjectTypeAttr: {
				Type:        schema.TypeString,
				Required:    true,
				ForceNew:    true,
				Description: "The type of the existing object to apply the security label to",
			},
			securityLabelProviderAttr: {
				Type:        schema.TypeString,
				Required:    true,
				ForceNew:    true,
				Description: "The provider to apply the security label for",
			},
			securityLabelLabelAttr: {
				Type:        schema.TypeString,
				Required:    true,
				ForceNew:    false,
				Description: "The label to be applied",
			},
		},
	}
}

func resourcePostgreSQLSecurityLabelCreate(db *DBConnection, d *schema.ResourceData) error {
	if !db.featureSupported(featureSecurityLabel) {
		return fmt.Errorf(
			"security Label is not supported for this Postgres version (%s)",
			db.version,
		)
	}
	log.Printf("[DEBUG] PostgreSQL security label Create")
	label := d.Get(securityLabelLabelAttr).(string)
	if err := resourcePostgreSQLSecurityLabelUpdateImpl(db, d, pq.QuoteLiteral(label)); err != nil {
		return err
	}

	d.SetId(generateSecurityLabelID(d))

	return resourcePostgreSQLSecurityLabelReadImpl(db, d)
}

func resourcePostgreSQLSecurityLabelUpdateImpl(db *DBConnection, d *schema.ResourceData, label string) error {
	b := bytes.NewBufferString("SECURITY LABEL ")

	objectType := d.Get(securityLabelObjectTypeAttr).(string)
	objectName := d.Get(securityLabelObjectNameAttr).(string)
	provider := d.Get(securityLabelProviderAttr).(string)
	fmt.Fprint(b, " FOR ", pq.QuoteIdentifier(provider))
	fmt.Fprint(b, " ON ", objectType, pq.QuoteIdentifier(objectName))
	fmt.Fprint(b, " IS ", label)

	if _, err := db.Exec(b.String()); err != nil {
		log.Printf("[WARN] PostgreSQL security label Create failed %s", err)
		return fmt.Errorf("could not create security label: %w", err)
	}
	return nil
}

func resourcePostgreSQLSecurityLabelRead(db *DBConnection, d *schema.ResourceData) error {
	if !db.featureSupported(featureSecurityLabel) {
		return fmt.Errorf(
			"security Label is not supported for this Postgres version (%s)",
			db.version,
		)
	}
	log.Printf("[DEBUG] PostgreSQL security label Read")

	return resourcePostgreSQLSecurityLabelReadImpl(db, d)
}

func resourcePostgreSQLSecurityLabelReadImpl(db *DBConnection, d *schema.ResourceData) error {
	objectType := d.Get(securityLabelObjectTypeAttr).(string)
	objectName := d.Get(securityLabelObjectNameAttr).(string)
	provider := d.Get(securityLabelProviderAttr).(string)

	txn, err := startTransaction(db.client, "")
	if err != nil {
		return err
	}
	defer deferredRollback(txn)

	query := "SELECT objtype, provider, objname, label FROM pg_seclabels WHERE objtype = $1 and objname = $2 and provider = $3"
	row := db.QueryRow(query, objectType, quoteIdentifier(objectName), quoteIdentifier(provider))

	var label, newObjectName, newProvider string
	err = row.Scan(&objectType, &newProvider, &newObjectName, &label)
	switch {
	case err == sql.ErrNoRows:
		log.Printf("[WARN] PostgreSQL security label for (%s '%s') with provider %s not found", objectType, objectName, provider)
		d.SetId("")
		return nil
	case err != nil:
		return fmt.Errorf("error reading security label: %w", err)
	}

	if quoteIdentifier(objectName) != newObjectName || quoteIdentifier(provider) != newProvider {
		// In reality, this should never happen, but if it does, we want to make sure that the state is in sync with the remote system
		// This will trigger a TF error saying that the provider has a bug if it ever happens
		objectName = newObjectName
		provider = newProvider
	}
	d.Set(securityLabelObjectTypeAttr, objectType)
	d.Set(securityLabelObjectNameAttr, objectName)
	d.Set(securityLabelProviderAttr, provider)
	d.Set(securityLabelLabelAttr, label)
	d.SetId(generateSecurityLabelID(d))

	return nil
}

func resourcePostgreSQLSecurityLabelDelete(db *DBConnection, d *schema.ResourceData) error {
	if !db.featureSupported(featureSecurityLabel) {
		return fmt.Errorf(
			"security Label is not supported for this Postgres version (%s)",
			db.version,
		)
	}
	log.Printf("[DEBUG] PostgreSQL security label Delete")

	if err := resourcePostgreSQLSecurityLabelUpdateImpl(db, d, "NULL"); err != nil {
		return err
	}

	d.SetId("")

	return nil
}

func resourcePostgreSQLSecurityLabelUpdate(db *DBConnection, d *schema.ResourceData) error {
	if !db.featureSupported(featureServer) {
		return fmt.Errorf(
			"security Label is not supported for this Postgres version (%s)",
			db.version,
		)
	}
	log.Printf("[DEBUG] PostgreSQL security label Update")

	label := d.Get(securityLabelLabelAttr).(string)
	if err := resourcePostgreSQLSecurityLabelUpdateImpl(db, d, pq.QuoteLiteral(label)); err != nil {
		return err
	}

	return resourcePostgreSQLSecurityLabelReadImpl(db, d)
}

func generateSecurityLabelID(d *schema.ResourceData) string {
	return strings.Join([]string{
		d.Get(securityLabelProviderAttr).(string),
		d.Get(securityLabelObjectTypeAttr).(string),
		d.Get(securityLabelObjectNameAttr).(string),
	}, ".")
}

func quoteIdentifier(s string) string {
	var result = s
	re := regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)
	if !re.MatchString(s) || s != strings.ToLower(s) {
		result = pq.QuoteIdentifier(s)
	}
	return result
}
