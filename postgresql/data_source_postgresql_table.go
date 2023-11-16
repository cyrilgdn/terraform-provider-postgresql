package postgresql

import (
	"database/sql"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
)

const tableDDLQueryTemplate = `
select 
  c.column_name, 
  c.udt_name, 
  tc.constraint_type, 
  c.numeric_precision, 
  c.numeric_scale, 
  c.character_maximum_length
from information_schema.columns c
left join information_schema.key_column_usage kcu
on kcu.table_catalog = c.table_catalog 
and kcu.table_schema = c.table_schema 
and kcu.table_name = c.table_name 
and kcu.column_name = c.column_name 
left join information_schema.table_constraints tc
on c.table_catalog = tc.table_catalog 
  and c.table_schema = tc.table_schema 
  and c.table_name = tc.table_name 
  and kcu.constraint_name = tc.constraint_name 
where c.table_catalog = $1
and c.table_schema = $2
and c.table_name = $3
`

// type tableDDLQueryInput struct {
// 	Database string
// 	Schema   string
// 	Table    string
// }

func dataSourcePostgreSQLDatabaseTable() *schema.Resource {
	return &schema.Resource{
		Read: PGResourceFunc(dataSourcePostgreSQLTableRead),
		Schema: map[string]*schema.Schema{
			"database": {
				Type:        schema.TypeString,
				Required:    true,
				ForceNew:    true,
				Description: "The PostgreSQL database which will be queried for table name",
			},
			"schema": {
				Type:        schema.TypeString,
				Required:    true,
				ForceNew:    true,
				Description: "The PostgreSQL schema which will be queried for table name",
			},
			"table": {
				Type:        schema.TypeString,
				Required:    true,
				ForceNew:    true,
				Description: "The PostgreSQL table which will be queried",
			},
			"columns": {
				Computed:    true,
				Description: "The list of PostgreSQL tables columns",
				Type:        schema.TypeList,
				Elem: &schema.Resource{
					Schema: map[string]*schema.Schema{
						"name": {
							Type:        schema.TypeString,
							Description: "Columns name",
							Required:    true,
						},
						"type": {
							Type:        schema.TypeString,
							Description: "Column type",
							Required:    true,
						},
						"is_primary_key": {
							Type:        schema.TypeBool,
							Description: "True if the column is a primary key",
							Required:    true,
						},
						"numeric_precision": {
							Type:        schema.TypeInt,
							Description: "Numeric precison for numeric type",
							Optional:    true,
						},
						"numeric_scale": {
							Type:        schema.TypeInt,
							Description: "Scale precison for numeric type",
							Optional:    true,
						},
						"character_maximum_length": {
							Type:        schema.TypeInt,
							Description: "Maximum length precison for varchar type",
							Optional:    true,
						},
					},
				},
			},
		},
	}
}

func dataSourcePostgreSQLTableRead(db *DBConnection, d *schema.ResourceData) error {
	database := d.Get("database").(string)
	schema := d.Get("schema").(string)
	table := d.Get("table").(string)

	txn, err := startTransaction(db.client, database)
	if err != nil {
		return err
	}
	defer deferredRollback(txn)

	stmt, err := txn.Prepare(tableDDLQueryTemplate)
	if err != nil {
		return err
	}

	rows, err := stmt.Query(database, schema, table)
	if err != nil {
		return err
	}
	defer rows.Close()

	tables := make([]interface{}, 0)
	for rows.Next() {
		var column_name sql.NullString
		var udt_name sql.NullString
		var constraint_type sql.NullString
		var numeric_precision sql.NullInt32
		var numeric_scale sql.NullInt32
		var character_maximum_length sql.NullInt32

		if err = rows.Scan(&column_name, &udt_name, &constraint_type, &numeric_precision, &numeric_scale, &character_maximum_length); err != nil {
			return fmt.Errorf("could not scan table output for database: %w", err)
		}

		result := make(map[string]interface{})
		result["name"] = column_name.String
		result["type"] = udt_name.String
		is_primary_key := false
		if constraint_type.String == "PRIMARY KEY" {
			is_primary_key = true
		}
		result["is_primary_key"] = is_primary_key
		result["numeric_precision"] = numeric_precision.Int32
		result["numeric_scale"] = numeric_scale.Int32
		result["character_maximum_length"] = character_maximum_length.Int32
		tables = append(tables, result)
	}

	d.Set("columns", tables)
	d.SetId(strings.Join([]string{database, schema, table}, "_"))

	return nil
}
