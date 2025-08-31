package postgresql

import (
	"fmt"
	"log"
	"strconv"
	"strings"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
)

var schemaQueries = map[string]string{
	"query_include_system_schemas": `
	SELECT schema_name
	FROM information_schema.schemata
	`,
	"query_exclude_system_schemas": `
	SELECT schema_name
	FROM information_schema.schemata
	WHERE schema_name NOT LIKE 'pg_%'
	AND schema_name <> 'information_schema'
	`,
}

const schemaPatternMatchingTarget = "schema_name"

func dataSourcePostgreSQLDatabaseSchemas() *schema.Resource {
	return &schema.Resource{
		Read: PGResourceFunc(dataSourcePostgreSQLSchemasRead),
		Schema: map[string]*schema.Schema{
			"database": {
				Type:        schema.TypeString,
				Required:    true,
				ForceNew:    true,
				Description: "The PostgreSQL database which will be queried for schema names",
			},
			"include_system_schemas": {
				Type:        schema.TypeBool,
				Default:     false,
				Optional:    true,
				Description: "Determines whether to include system schemas (pg_ prefix and information_schema). 'public' will always be included.",
			},
			"like_any_patterns": {
				Type:        schema.TypeList,
				Optional:    true,
				Elem:        &schema.Schema{Type: schema.TypeString},
				MinItems:    0,
				Description: "Expression(s) which will be pattern matched in the query using the PostgreSQL LIKE ANY operator",
			},
			"like_all_patterns": {
				Type:        schema.TypeList,
				Optional:    true,
				Elem:        &schema.Schema{Type: schema.TypeString},
				MinItems:    0,
				Description: "Expression(s) which will be pattern matched in the query using the PostgreSQL LIKE ALL operator",
			},
			"not_like_all_patterns": {
				Type:        schema.TypeList,
				Optional:    true,
				Elem:        &schema.Schema{Type: schema.TypeString},
				MinItems:    0,
				Description: "Expression(s) which will be pattern matched in the query using the PostgreSQL NOT LIKE ALL operator",
			},
			"regex_pattern": {
				Type:        schema.TypeString,
				Optional:    true,
				Description: "Expression which will be pattern matched in the query using the PostgreSQL ~ (regular expression match) operator",
			},
			"schemas": {
				Type:        schema.TypeSet,
				Computed:    true,
				Elem:        &schema.Schema{Type: schema.TypeString},
				Set:         schema.HashString,
				Description: "The list of PostgreSQL schemas retrieved by this data source",
			},
		},
	}
}

func dataSourcePostgreSQLSchemasRead(db *DBConnection, d *schema.ResourceData) error {
	database := d.Get("database").(string)

	txn, err := startTransaction(db.client, database)
	if err != nil {
		return err
	}
	defer deferredRollback(txn)

	includeSystemSchemas := d.Get("include_system_schemas").(bool)

	var query string
	var queryConcatKeyword string
	if includeSystemSchemas {
		query = schemaQueries["query_include_system_schemas"]
		queryConcatKeyword = queryConcatKeywordWhere
	} else {
		query = schemaQueries["query_exclude_system_schemas"]
		queryConcatKeyword = queryConcatKeywordAnd
	}

	query = applySchemaDataSourceQueryFilters(query, queryConcatKeyword, d)

	rows, err := txn.Query(query)
	if err != nil {
		return err
	}
	defer func() {
		if err := rows.Close(); err != nil {
			log.Printf("error closing rows: %v", err)
		}
	}()

	schemas := []string{}
	for rows.Next() {
		var schema string

		if err = rows.Scan(&schema); err != nil {
			return fmt.Errorf("could not scan schema name for database: %w", err)
		}
		schemas = append(schemas, schema)
	}

	d.Set("schemas", stringSliceToSet(schemas))
	d.SetId(generateDataSourceSchemasID(d, database))

	return nil
}

func generateDataSourceSchemasID(d *schema.ResourceData, databaseName string) string {
	return strings.Join([]string{
		databaseName, strconv.FormatBool(d.Get("include_system_schemas").(bool)),
		generatePatternArrayString(d.Get("like_any_patterns").([]interface{}), queryArrayKeywordAny),
		generatePatternArrayString(d.Get("like_all_patterns").([]interface{}), queryArrayKeywordAll),
		generatePatternArrayString(d.Get("not_like_all_patterns").([]interface{}), queryArrayKeywordAll),
		d.Get("regex_pattern").(string),
	}, "_")
}

func applySchemaDataSourceQueryFilters(query string, queryConcatKeyword string, d *schema.ResourceData) string {
	filters := []string{}
	filters = append(filters, applyPatternMatchingToQuery(schemaPatternMatchingTarget, d)...)

	return finalizeQueryWithFilters(query, queryConcatKeyword, filters)
}
