package postgresql

import (
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"strconv"
	"strings"
)

func dataSourcePostgreSQLDatabaseConnection() *schema.Resource {
	return &schema.Resource{
		Read: PGResourceFunc(dataSourcePostgreSQLConnectionRead),
		Schema: map[string]*schema.Schema{
			"host": {
				Type:        schema.TypeString,
				Computed:    true,
				Description: "The current connected PostgreSQL server hostname",
			},
			"port": {
				Type:        schema.TypeInt,
				Computed:    true,
				Description: "The current connected PostgreSQL server port",
			},
			"scheme": {
				Type:        schema.TypeString,
				Computed:    true,
				Description: "TThe current connected PostgreSQL server scheme",
			},
			"username": {
				Type:        schema.TypeString,
				Computed:    true,
				Description: "The current connected username of the PostgreSQL server",
			},
			"database_username": {
				Type:        schema.TypeString,
				Computed:    true,
				Description: "The current connected username of the PostgreSQL server",
			},
			"version": {
				Type:        schema.TypeString,
				Computed:    true,
				Description: "The current connected PostgreSQL server version",
			},
			"database": {
				Type:        schema.TypeString,
				Computed:    true,
				Description: "The current connected PostgreSQL server database",
			},
		},
	}
}

func dataSourcePostgreSQLConnectionRead(db *DBConnection, d *schema.ResourceData) error {
	d.Set("host", db.client.config.Host)
	d.Set("port", db.client.config.Port)
	d.Set("scheme", db.client.config.Scheme)
	d.Set("username", db.client.config.Username)
	d.Set("database_username", db.client.config.DatabaseUsername)
	d.Set("version", db.version.String())
	d.Set("database", db.client.databaseName)

	d.SetId(strings.Join([]string{
		db.client.config.Host,
		strconv.Itoa(db.client.config.Port),
		db.client.config.Scheme,
		db.client.config.Username,
		db.client.config.DatabaseUsername,
		db.version.String(),
		db.client.databaseName,
	}, "_"))
	return nil
}
