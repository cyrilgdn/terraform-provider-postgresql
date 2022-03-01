package postgresql

import (
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
)

const (
	sequenceQuery = `
	SELECT sequence_name
	FROM information_schema.sequences
	`
	sequencePatternMatchingTarget = "sequence_name"
	sequenceSchemaKeyword         = "sequence_schema"
	sequenceTypeKeyword           = "data_type"
)

func dataSourcePostgreSQLDatabaseSequences() *schema.Resource {
	return &schema.Resource{
		Read: PGResourceFunc(dataSourcePostgreSQLSequencesRead),
		Schema: map[string]*schema.Schema{
			"database": {
				Type:        schema.TypeString,
				Required:    true,
				ForceNew:    true,
				Description: "The PostgreSQL database which will be queried for sequence names",
			},
			"schemas": {
				Type:        schema.TypeList,
				Optional:    true,
				Elem:        &schema.Schema{Type: schema.TypeString},
				MinItems:    0,
				Description: "The PostgreSQL schema(s) which will be queried for sequence names. Queries all schemas in the database by default",
			},
			"data_types": {
				Type:        schema.TypeList,
				Optional:    true,
				Elem:        &schema.Schema{Type: schema.TypeString},
				MinItems:    0,
				Description: "The PostgreSQL sequence data types which will be queried for sequence names. Includes all sequence data types by default.",
			},
			"like_any_patterns": {
				Type:        schema.TypeList,
				Optional:    true,
				Elem:        &schema.Schema{Type: schema.TypeString},
				MinItems:    0,
				Description: "Expression(s) which will be pattern matched against sequence names in the query using the PostgreSQL LIKE ANY operator",
			},
			"like_all_patterns": {
				Type:        schema.TypeList,
				Optional:    true,
				Elem:        &schema.Schema{Type: schema.TypeString},
				MinItems:    0,
				Description: "Expression(s) which will be pattern matched against sequence names in the query using the PostgreSQL LIKE ALL operator",
			},
			"not_like_all_patterns": {
				Type:        schema.TypeList,
				Optional:    true,
				Elem:        &schema.Schema{Type: schema.TypeString},
				MinItems:    0,
				Description: "Expression(s) which will be pattern matched against sequence names in the query using the PostgreSQL NOT LIKE ALL operator",
			},
			"regex_pattern": {
				Type:        schema.TypeString,
				Optional:    true,
				Description: "Expression which will be pattern matched against sequence names in the query using the PostgreSQL ~ (regular expression match) operator",
			},
			"sequences": {
				Type:        schema.TypeSet,
				Computed:    true,
				Elem:        &schema.Schema{Type: schema.TypeString},
				Set:         schema.HashString,
				Description: "The list of PostgreSQL sequence names retrieved by this data source. Note that this returns a set, so duplicate table names across different schemas will be consolidated.",
			},
		},
	}
}

func dataSourcePostgreSQLSequencesRead(db *DBConnection, d *schema.ResourceData) error {
	database := d.Get("database").(string)

	txn, err := startTransaction(db.client, database)
	if err != nil {
		return err
	}
	defer deferredRollback(txn)

	query := sequenceQuery
	queryConcatKeyword := queryConcatKeywordWhere

	query = applySchemaAndTypeFilteringToQuery(query, &queryConcatKeyword, sequenceSchemaKeyword, sequenceTypeKeyword, d.Get("schemas").([]interface{}), d.Get("data_types").([]interface{}))
	query = applyOptionalPatternMatchingToQuery(query, sequencePatternMatchingTarget, &queryConcatKeyword, d)

	rows, err := txn.Query(query)
	if err != nil {
		return err
	}
	defer rows.Close()

	sequences := []string{}
	for rows.Next() {
		var sequence string

		if err = rows.Scan(&sequence); err != nil {
			return fmt.Errorf("could not scan sequence name for database: %w", err)
		}
		sequences = append(sequences, sequence)
	}

	d.Set("sequences", stringSliceToSet(sequences))
	d.SetId(generateDataSourceSequencesID(d, database))

	return nil
}

func generateDataSourceSequencesID(d *schema.ResourceData, databaseName string) string {
	return strings.Join([]string{
		databaseName,
		generatePatternArrayString(d.Get("schemas").([]interface{}), queryArrayKeywordAny),
		generatePatternArrayString(d.Get("data_types").([]interface{}), queryArrayKeywordAny),
		generatePatternArrayString(d.Get("like_any_patterns").([]interface{}), queryArrayKeywordAny),
		generatePatternArrayString(d.Get("like_all_patterns").([]interface{}), queryArrayKeywordAll),
		generatePatternArrayString(d.Get("not_like_all_patterns").([]interface{}), queryArrayKeywordAll),
		d.Get("regex_pattern").(string),
	}, "_")
}
