package postgresql

import (
	"crypto/sha256"
	"fmt"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
)

// Based on implementation from https://github.com/ricochet1k/terraform-provider-postgresql/commit/e351e932b97142ab7b55b1b943b0864a3e8953be
// Original work by @ricochet1k
func dataSourcePostgreSQLQuery() *schema.Resource {
	return &schema.Resource{
		Read: PGResourceFunc(dataSourcePostgreSQLQueryRead),
		Schema: map[string]*schema.Schema{
			"database": {
				Type:        schema.TypeString,
				Required:    true,
				ForceNew:    true,
				Description: "The PostgreSQL database which will be queried for table names",
			},
			"query": {
				Type:        schema.TypeString,
				Required:    true,
				Description: "The PostgreSQL query",
			},
			"args": {
				Type:        schema.TypeList,
				Optional:    true,
				Elem:        &schema.Schema{Type: schema.TypeString},
				Description: "The values to fill in for any placeholders (?)",
			},
			"columns": {
				Type:     schema.TypeList,
				Computed: true,
				Elem: &schema.Resource{
					Schema: map[string]*schema.Schema{
						"name": {
							Type:     schema.TypeString,
							Computed: true,
						},
						"type": {
							Type:     schema.TypeString,
							Computed: true,
						},
					},
				},
				Description: "The columns returned by the query.",
			},
			"rows": {
				Type:        schema.TypeList,
				Computed:    true,
				Elem:        &schema.Schema{Type: schema.TypeMap},
				Description: "The rows returned by the query.",
			},
		},
	}
}

func dataSourcePostgreSQLQueryRead(db *DBConnection, d *schema.ResourceData) error {

	database := d.Get("database").(string)

	txn, err := startTransaction(db.client, database)
	if err != nil {
		return err
	}
	defer deferredRollback(txn)

	query := d.Get("query").(string)
	rawargs := d.Get("args")

	args := []interface{}{}
	if rawargs != nil {
		if argsList, ok := rawargs.([]interface{}); ok {
			args = argsList
		}
	}

	rows, err := txn.Query(query, args...)
	if err != nil {
		return err
	}
	defer rows.Close()

	columns, err := rows.Columns()
	if err != nil {
		return err
	}

	columnTypes, err := rows.ColumnTypes()
	if err != nil {
		return err
	}

	output_columns := make([]interface{}, len(columns))
	for i, col := range columns {
		output_columns[i] = map[string]interface{}{
			"name": col,
			"type": columnTypes[i].DatabaseTypeName(),
		}
	}
	d.Set("columns", output_columns)

	rowdata := make([]interface{}, len(columns))
	rowptrs := make([]interface{}, len(columns))
	for i := range rowptrs {
		rowptrs[i] = &rowdata[i]
	}

	output_rows := make([]interface{}, 0)
	for rows.Next() {
		if err = rows.Scan(rowptrs...); err != nil {
			return fmt.Errorf("could not scan output for query: %w", err)
		}

		result := make(map[string]interface{}, len(columns))
		for i, col := range columns {
			result[col] = fmt.Sprint(rowdata[i])
		}
		output_rows = append(output_rows, result)
	}

	// Check for errors from row iteration
	if err = rows.Err(); err != nil {
		return fmt.Errorf("error during row iteration: %w", err)
	}

	d.Set("rows", output_rows)
	d.SetId(generateDataSourceQueryID(database, query))

	return nil
}

func generateDataSourceQueryID(databaseName, query string) string {
	// Use a hash to avoid potential ID collisions and length issues
	h := sha256.New()
	h.Write([]byte(databaseName + "|" + query))
	return fmt.Sprintf("%s_query_%x", databaseName, h.Sum(nil)[:8])
}