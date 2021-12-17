package postgresql

// Use Postgres as SQL driver
import (
	"fmt"
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

var likePatternQuery = "s.schema_name LIKE"
var notLikePatternQuery = "s.schema_name NOT LIKE"
var similarToPatternQuery = "s.schema SIMILAR TO"
var notSimilarToPatternQuery = "s.schema NOT SIMILAR TO"

func dataSourcePostgreSQLDatabaseSchemas() *schema.Resource {
	return &schema.Resource{
		Read: PGResourceFunc(dataSourcePostgreSQLSchemasRead),
		Schema: map[string]*schema.Schema{
			"database": {
				Type:        schema.TypeString,
				Required:    true,
				ForceNew:    true,
				Description: "The database to grant privileges on for this role",
			},
			"include_system_schemas": {
				Type:        schema.TypeBool,
				Default:     false,
				Optional:    true,
				Description: "Whether to include system schemas (pg_ prefix and information_schema)",
			},
			"like_pattern": {
				Type:        schema.TypeString,
				Optional:    true,
				Description: "Optional expression which will be pattern matched in the query using the PostgreSQL LIKE operator",
			},
			"not_like_pattern": {
				Type:        schema.TypeString,
				Optional:    true,
				Description: "Optional expression which will be pattern matched in the query using the PostgreSQL NOT LIKE operator",
			},
			"similar_to_pattern": {
				Type:        schema.TypeString,
				Optional:    true,
				Description: "Optional expression which will be pattern matched in the query using the PostgreSQL SIMILAR TO operator",
			},
			"not_similar_to_pattern": {
				Type:        schema.TypeString,
				Optional:    true,
				Description: "Optional expression which will be pattern matched in the query using the PostgreSQL NOT SIMILAR TO operator",
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

	d.SetId(database + " data block") //improve the Id

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

	//query = applyOptionalPatternMatchingToQuery(query, d)
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

	// log.Println("something")
	// log.Println(database)
	// log.Println(query)

	// for schemaIn := range schemas {
	// 	log.Println(schemas[schemaIn])
	// }

	// log.Println("after loop")

	return nil
}

func applyOptionalPatternMatchingToQuery(query string, d *schema.ResourceData) string {
	likePattern := d.Get("like_pattern")
	notLikePattern := d.Get("not_like_pattern")
	similarToPattern := d.Get("similar_to_pattern")
	notSimilarToPattern := d.Get("not_similar_to_pattern")

	if likePattern != nil {
		query = concatenateQueryWithPatternMatching(query, likePatternQuery, likePattern.(string))
	}
	if notLikePattern != nil {
		query = concatenateQueryWithPatternMatching(query, notLikePatternQuery, notLikePattern.(string))
	}
	if similarToPattern != nil {
		query = concatenateQueryWithPatternMatching(query, similarToPatternQuery, similarToPattern.(string))
	}
	if notSimilarToPattern != nil {
		query = concatenateQueryWithPatternMatching(query, notSimilarToPatternQuery, notSimilarToPattern.(string))
	}

	return query
}

func concatenateQueryWithPatternMatching(query string, additionalQuery string, pattern string) string {
	var keyword string
	if strings.Contains(query, "WHERE") { //hacky and fragile use regex instead
		keyword = "WHERE"
	} else {
		keyword = "AND"
	}

	return fmt.Sprintf("%s %s %s '%s'", query, keyword, additionalQuery, pattern)
}
