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
	FROM information_schema.schemata s
	`,
	"query_exclude_system_schemas": `
	SELECT schema_name
	FROM information_schema.schemata s
	WHERE s.schema_name NOT LIKE 'pg_%'
	AND s.schema_name <> 'information_schema'
	`,
}

const queryArrayKeywordAny = "ANY"
const queryArrayKeywordAll = "ALL"

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
				Type:        schema.TypeList,
				Computed:    true,
				Elem:        &schema.Schema{Type: schema.TypeString},
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
	if includeSystemSchemas {
		query = schemaQueries["query_include_system_schemas"]
	} else {
		query = schemaQueries["query_exclude_system_schemas"]
	}

	query = applyOptionalPatternMatchingToQuery(query, !includeSystemSchemas, d)

	rows, err := txn.Query(query)
	if err != nil {
		return err
	}
	defer rows.Close()

	schemas := []string{}
	for rows.Next() {
		var schema string

		if err = rows.Scan(&schema); err != nil {
			return fmt.Errorf("could not scan schema name for database: %w", err)
		}
		schemas = append(schemas, schema)
	}

	d.Set("schemas", schemas)
	d.SetId(generateDataSourceSchemasID(d, database))

	return nil
}

func applyOptionalPatternMatchingToQuery(query string, queryContainsWhere bool, d *schema.ResourceData) string {
	likeAnyPatterns := d.Get("like_any_patterns").([]interface{})
	likeAllPatterns := d.Get("like_all_patterns").([]interface{})
	notLikeAllPatterns := d.Get("not_like_all_patterns").([]interface{})
	regexPattern := d.Get("regex_pattern").(string)

	likePatternQuery := "s.schema_name LIKE"
	notLikePatternQuery := "s.schema_name NOT LIKE"
	regexPatternQuery := "s.schema_name ~"

	if len(likeAnyPatterns) > 0 {
		query = concatenateQueryWithPatternMatching(query, likePatternQuery, generatePatternArrayString(likeAnyPatterns, queryArrayKeywordAny), &queryContainsWhere)
	}
	if len(likeAllPatterns) > 0 {
		query = concatenateQueryWithPatternMatching(query, likePatternQuery, generatePatternArrayString(likeAllPatterns, queryArrayKeywordAll), &queryContainsWhere)
	}
	if len(notLikeAllPatterns) > 0 {
		query = concatenateQueryWithPatternMatching(query, notLikePatternQuery, generatePatternArrayString(notLikeAllPatterns, queryArrayKeywordAll), &queryContainsWhere)
	}
	if regexPattern != "" {
		query = concatenateQueryWithPatternMatching(query, regexPatternQuery, fmt.Sprintf("'%s'", regexPattern), &queryContainsWhere)
	}

	log.Println(query)
	return query
}

func generatePatternArrayString(patterns []interface{}, queryArrayKeyword string) string {
	formattedPatterns := []string{}

	for _, pattern := range patterns {
		formattedPatterns = append(formattedPatterns, fmt.Sprintf("'%s'", pattern.(string)))
		log.Println(pattern.(string))
	}
	return fmt.Sprintf("%s (array[%s])", queryArrayKeyword, strings.Join(formattedPatterns, ","))

}

func concatenateQueryWithPatternMatching(query string, additionalQuery string, pattern string, queryContainsWhere *bool) string {
	var keyword string
	if *queryContainsWhere {
		keyword = "AND"
	} else {
		keyword = "WHERE"
		*queryContainsWhere = true
	}

	return fmt.Sprintf("%s %s %s %s", query, keyword, additionalQuery, pattern)
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
