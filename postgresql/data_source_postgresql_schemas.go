package postgresql

// Use Postgres as SQL driver
import (
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
)

func dataSourcePostgreSQLDatabaseSchemas() *schema.Resource {
	return &schema.Resource{
		Read: PGResourceFunc(dataSourcePostgreSQLDbSchemasRead),

		Schema: map[string]*schema.Schema{
			"include_reserved_schemas": {
				Type:     schema.TypeBool,
				Default:  false,
				Optional: true,
			},
			"schemas": {
				Type:     schema.TypeList,
				Computed: true,
				Elem: &schema.Schema{
					Type: schema.TypeString,
				},
			},
		},
	}
}

func dataSourcePostgreSQLDbSchemasRead(db *DBConnection, d *schema.ResourceData) error {
	database := d.Get("database").(string)

	txn, err := startTransaction(db.client, database)
	if err != nil {
		return err
	}
	defer deferredRollback(txn)

	return nil
}
