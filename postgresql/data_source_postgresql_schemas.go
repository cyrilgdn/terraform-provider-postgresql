package postgresql

import (
	"fmt"
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
	AND s.schema_name <> 'public'
	`,
}

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
				Description: "Determines whether to include system schemas (pg_ prefix, public and information_schema)",
			},
			"like_pattern": {
				Type:        schema.TypeString,
				Optional:    true,
				Description: "Expression which will be pattern matched in the query using the PostgreSQL LIKE operator",
			},
			"not_like_pattern": {
				Type:        schema.TypeString,
				Optional:    true,
				Description: "Expression which will be pattern matched in the query using the PostgreSQL NOT LIKE operator",
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
	likePattern := d.Get("like_pattern").(string)
	notLikePattern := d.Get("not_like_pattern").(string)
	regexPattern := d.Get("regex_pattern").(string)

	likePatternQuery := "s.schema_name LIKE"
	notLikePatternQuery := "s.schema_name NOT LIKE"
	regexPatternQuery := "s.schema_name ~"

	if likePattern != "" {
		query = concatenateQueryWithPatternMatching(query, likePatternQuery, likePattern, &queryContainsWhere)
	}
	if notLikePattern != "" {
		query = concatenateQueryWithPatternMatching(query, notLikePatternQuery, notLikePattern, &queryContainsWhere)
	}
	if regexPattern != "" {
		query = concatenateQueryWithPatternMatching(query, regexPatternQuery, regexPattern, &queryContainsWhere)
	}

	return query
}

func concatenateQueryWithPatternMatching(query string, additionalQuery string, pattern string, queryContainsWhere *bool) string {
	var keyword string
	if *queryContainsWhere {
		keyword = "AND"
	} else {
		keyword = "WHERE"
		*queryContainsWhere = true
	}

	return fmt.Sprintf("%s %s %s '%s'", query, keyword, additionalQuery, pattern)
}

func generateDataSourceSchemasID(d *schema.ResourceData, databaseName string) string {
	return strings.Join([]string{
		databaseName, strconv.FormatBool(d.Get("include_system_schemas").(bool)),
		d.Get("like_pattern").(string), d.Get("not_like_pattern").(string), d.Get("regex_pattern").(string),
	}, "_")
}
