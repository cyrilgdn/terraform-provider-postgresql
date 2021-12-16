package postgresql

// Use Postgres as SQL driver
import (
	"fmt"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/lib/pq"
)

var schemaQueries = map[string]string{
	"query_include_reserved_schemas": `
	SELECT schema_name
	FROM information_schema.schemata
	`,
	"query_exclude_reserved_schemas": `
	SELECT schema_name
	FROM information_schema.schemata
	WHERE s.schema_name NOT LIKE 'pg_%'
	AND s.schema_name <> 'information_schema'
	`,
}

var likePatternQuery = "AND s.schema_name LIKE "
var notLikePatternQuery = "AND s.schema_name NOT LIKE "
var similarToPatternQuery = "AND s.schema SIMILAR TO "
var notSimilarToPatternQuery = "AND s.schema NOT SIMILAR TO "

func dataSourcePostgreSQLDatabaseSchemas() *schema.Resource {
	return &schema.Resource{
		Read: PGResourceFunc(dataSourcePostgreSQLSchemasRead),
		Schema: map[string]*schema.Schema{
			"include_system_schemas": {
				Type:        schema.TypeBool,
				Default:     false,
				Optional:    true,
				Description: "Whether to include system schemas (pg_ prefix and information_schema)",
			},
			"like_pattern_expression": {
				Type:        schema.TypeString,
				Optional:    true,
				Description: "Optional expression which will be pattern matched in the query using the PostgreSQL LIKE operator",
			},
			"not_like_pattern_expression": {
				Type:        schema.TypeString,
				Optional:    true,
				Description: "Optional expression which will be pattern matched in the query using the PostgreSQL NOT LIKE operator",
			},
			"similar_to_pattern_expression": {
				Type:        schema.TypeString,
				Optional:    true,
				Description: "Optional expression which will be pattern matched in the query using the PostgreSQL SIMILAR TO operator",
			},
			"not_similar_to_pattern_expression": {
				Type:        schema.TypeString,
				Optional:    true,
				Description: "Optional expression which will be pattern matched in the query using the PostgreSQL NOT SIMILAR TO operator",
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

	includeReservedSchemas := d.Get("include_reserved_schemas").(bool)

	var query string
	if includeReservedSchemas {
		query = schemaQueries["query_include_reserved_schemas"]
	} else {
		query = schemaQueries["query_exclude_reserved_schemas"]
	}
	query = applyOptionalPatternMatchingToQuery(query, d)

	var wildcardSchemas pq.ByteaArray
	rows, err := txn.Query(query)

	if err != nil {
		return err
	}

	rows.Scan(&wildcardSchemas)
	d.Set("schemas", pgArrayToSet(wildcardSchemas))

	return nil
}

func applyOptionalPatternMatchingToQuery(query string, d *schema.ResourceData) string {
	likePattern := d.Get("like_pattern_expression").(string)
	notLikePattern := d.Get("not_like_pattern_expression").(string)
	similarToPattern := d.Get("similar_to_pattern_expression").(string)
	notSimilarToPattern := d.Get("not_similar_to_pattern_expression").(string)

	if likePattern != "" {
		query = concatenateQueryWithPatternMatching(query, likePatternQuery, likePattern)
	}
	if notLikePattern != "" {
		query = concatenateQueryWithPatternMatching(query, notLikePatternQuery, notLikePattern)
	}
	if similarToPattern != "" {
		query = concatenateQueryWithPatternMatching(query, similarToPatternQuery, similarToPattern)
	}
	if notSimilarToPattern != "" {
		query = concatenateQueryWithPatternMatching(query, notSimilarToPatternQuery, notSimilarToPattern)
	}

	return query
}

func concatenateQueryWithPatternMatching(query string, additionalQuery string, pattern string) string {
	return fmt.Sprintf("%s %s'%s'", query, additionalQuery, pattern)
}
