package postgresql

import (
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
)

const (
	tableQuery = `
	SELECT table_name, table_schema, table_type
	FROM information_schema.tables
	`
	tablePatternMatchingTarget = "table_name"
	tableSchemaKeyword         = "table_schema"
	tableTypeKeyword           = "table_type"
)

func dataSourcePostgreSQLDatabaseTables() *schema.Resource {
	return &schema.Resource{
		Read: PGResourceFunc(dataSourcePostgreSQLTablesRead),
		Schema: map[string]*schema.Schema{
			"database": {
				Type:        schema.TypeString,
				Required:    true,
				ForceNew:    true,
				Description: "The PostgreSQL database which will be queried for table names",
			},
			"schemas": {
				Type:        schema.TypeList,
				Optional:    true,
				Elem:        &schema.Schema{Type: schema.TypeString},
				MinItems:    0,
				Description: "The PostgreSQL schema(s) which will be queried for table names. Queries all schemas in the database by default",
			},
			"table_types": {
				Type:        schema.TypeList,
				Optional:    true,
				Elem:        &schema.Schema{Type: schema.TypeString},
				MinItems:    0,
				Description: "The PostgreSQL table types which will be queried for table names. Includes all table types by default. Use 'BASE TABLE' for normal tables only",
			},
			"like_any_patterns": {
				Type:        schema.TypeList,
				Optional:    true,
				Elem:        &schema.Schema{Type: schema.TypeString},
				MinItems:    0,
				Description: "Expression(s) which will be pattern matched against table names in the query using the PostgreSQL LIKE ANY operator",
			},
			"like_all_patterns": {
				Type:        schema.TypeList,
				Optional:    true,
				Elem:        &schema.Schema{Type: schema.TypeString},
				MinItems:    0,
				Description: "Expression(s) which will be pattern matched against table names in the query using the PostgreSQL LIKE ALL operator",
			},
			"not_like_all_patterns": {
				Type:        schema.TypeList,
				Optional:    true,
				Elem:        &schema.Schema{Type: schema.TypeString},
				MinItems:    0,
				Description: "Expression(s) which will be pattern matched against table names in the query using the PostgreSQL NOT LIKE ALL operator",
			},
			"regex_pattern": {
				Type:        schema.TypeString,
				Optional:    true,
				Description: "Expression which will be pattern matched against table names in the query using the PostgreSQL ~ (regular expression match) operator",
			},
			"tables": {
				Type:     schema.TypeList,
				Computed: true,
				Elem: &schema.Resource{
					Schema: map[string]*schema.Schema{
						"object_name": {
							Type:     schema.TypeString,
							Computed: true,
						},
						"schema_name": {
							Type:     schema.TypeString,
							Computed: true,
						},
						"table_type": {
							Type:     schema.TypeString,
							Computed: true,
						},
					},
				},
				Description: "The list of PostgreSQL tables retrieved by this data source. Note that this returns a set, so duplicate table names across different schemas will be consolidated.",
			},
		},
	}
}

func dataSourcePostgreSQLTablesRead(db *DBConnection, d *schema.ResourceData) error {
	database := d.Get("database").(string)

	txn, err := startTransaction(db.client, database)
	if err != nil {
		return err
	}
	defer deferredRollback(txn)

	query := tableQuery
	queryConcatKeyword := queryConcatKeywordWhere

	query = applySchemaAndTypeFilteringToQuery(query, &queryConcatKeyword, tableSchemaKeyword, tableTypeKeyword, d.Get("schemas").([]interface{}), d.Get("table_types").([]interface{}))
	query = applyOptionalPatternMatchingToQuery(query, tablePatternMatchingTarget, &queryConcatKeyword, d)

	rows, err := txn.Query(query)
	if err != nil {
		return err
	}
	defer rows.Close()

	tables := make([]interface{}, 0)
	for rows.Next() {
		var object_name string
		var schema_name string
		var table_type string

		if err = rows.Scan(&object_name, &schema_name, &table_type); err != nil {
			return fmt.Errorf("could not scan table output for database: %w", err)
		}

		result := make(map[string]interface{})
		result["object_name"] = object_name
		result["schema_name"] = schema_name
		result["table_type"] = table_type
		tables = append(tables, result)
	}

	d.Set("tables", tables)
	d.SetId(generateDataSourceTablesID(d, database))

	return nil
}

func generateDataSourceTablesID(d *schema.ResourceData, databaseName string) string {
	return strings.Join([]string{
		databaseName,
		generatePatternArrayString(d.Get("schemas").([]interface{}), queryArrayKeywordAny),
		generatePatternArrayString(d.Get("table_types").([]interface{}), queryArrayKeywordAny),
		generatePatternArrayString(d.Get("like_any_patterns").([]interface{}), queryArrayKeywordAny),
		generatePatternArrayString(d.Get("like_all_patterns").([]interface{}), queryArrayKeywordAll),
		generatePatternArrayString(d.Get("not_like_all_patterns").([]interface{}), queryArrayKeywordAll),
		d.Get("regex_pattern").(string),
	}, "_")
}
