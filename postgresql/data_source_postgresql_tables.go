package postgresql

import (
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
)

const (
	tableQuery = `
	SELECT table_name
	FROM information_schema.tables
	`
	tablePatternMatchingTarget = "table_name"
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
				Type:        schema.TypeSet,
				Computed:    true,
				Elem:        &schema.Schema{Type: schema.TypeString},
				Set:         schema.HashString,
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

	query = applySchemaAndTableTypeFilteringToQuery(query, &queryConcatKeyword, d)
	query = applyOptionalPatternMatchingToQuery(query, tablePatternMatchingTarget, &queryConcatKeyword, d)

	rows, err := txn.Query(query)
	if err != nil {
		return err
	}
	defer rows.Close()

	tables := []string{}
	for rows.Next() {
		var table string

		if err = rows.Scan(&table); err != nil {
			return fmt.Errorf("could not scan table name for database: %w", err)
		}
		tables = append(tables, table)
	}

	d.Set("tables", stringSliceToSet(tables))
	d.SetId(generateDataSourceTablesID(d, database))

	return nil
}

func applySchemaAndTableTypeFilteringToQuery(query string, queryConcatKeyword *string, d *schema.ResourceData) string {
	schemas := d.Get("schemas").([]interface{})
	tableTypes := d.Get("table_types").([]interface{})

	if len(schemas) > 0 {
		query = fmt.Sprintf("%s %s table_schema = %s", query, *queryConcatKeyword, generatePatternArrayString(schemas, queryArrayKeywordAny))
		*queryConcatKeyword = queryConcatKeywordAnd
	}
	if len(tableTypes) > 0 {
		query = fmt.Sprintf("%s %s table_type = %s", query, *queryConcatKeyword, generatePatternArrayString(tableTypes, queryArrayKeywordAny))
		*queryConcatKeyword = queryConcatKeywordAnd
	}

	return query
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
