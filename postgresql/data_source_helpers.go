package postgresql

import (
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
)

const (
	queryConcatKeywordWhere = "WHERE"
	queryConcatKeywordAnd   = "AND"
	queryArrayKeywordAny    = "ANY"
	queryArrayKeywordAll    = "ALL"
	likePatternQuery        = "LIKE"
	notLikePatternQuery     = "NOT LIKE"
	regexPatternQuery       = "~"
)

func applyOptionalPatternMatchingToQuery(query string, patternMatchingTarget string, queryConcatKeyword *string, d *schema.ResourceData) string {
	likeAnyPatterns := d.Get("like_any_patterns").([]interface{})
	likeAllPatterns := d.Get("like_all_patterns").([]interface{})
	notLikeAllPatterns := d.Get("not_like_all_patterns").([]interface{})
	regexPattern := d.Get("regex_pattern").(string)

	if len(likeAnyPatterns) > 0 {
		query = finalizeQueryWithPatternMatching(query, patternMatchingTarget, likePatternQuery, generatePatternArrayString(likeAnyPatterns, queryArrayKeywordAny), queryConcatKeyword)
	}
	if len(likeAllPatterns) > 0 {
		query = finalizeQueryWithPatternMatching(query, patternMatchingTarget, likePatternQuery, generatePatternArrayString(likeAllPatterns, queryArrayKeywordAll), queryConcatKeyword)
	}
	if len(notLikeAllPatterns) > 0 {
		query = finalizeQueryWithPatternMatching(query, patternMatchingTarget, notLikePatternQuery, generatePatternArrayString(notLikeAllPatterns, queryArrayKeywordAll), queryConcatKeyword)
	}
	if regexPattern != "" {
		query = finalizeQueryWithPatternMatching(query, patternMatchingTarget, regexPatternQuery, fmt.Sprintf("'%s'", regexPattern), queryConcatKeyword)
	}

	return query
}

func generatePatternArrayString(patterns []interface{}, queryArrayKeyword string) string {
	formattedPatterns := []string{}

	for _, pattern := range patterns {
		formattedPatterns = append(formattedPatterns, fmt.Sprintf("'%s'", pattern.(string)))
	}
	return fmt.Sprintf("%s (array[%s])", queryArrayKeyword, strings.Join(formattedPatterns, ","))
}

func applyEqualsAnyFilteringToQuery(query string, queryConcatKeyword *string, objectKeyword string, objects []interface{}) string {
	if len(objects) > 0 {
		query = fmt.Sprintf("%s %s %s = %s", query, *queryConcatKeyword, objectKeyword, generatePatternArrayString(objects, queryArrayKeywordAny))
		*queryConcatKeyword = queryConcatKeywordAnd
	}

	return query
}

func finalizeQueryWithPatternMatching(query string, patternMatchingTarget string, additionalQuery string, pattern string, queryConcatKeyword *string) string {
	finalizedQuery := fmt.Sprintf("%s %s %s %s %s", query, *queryConcatKeyword, patternMatchingTarget, additionalQuery, pattern)

	//Set the query concatenation keyword from WHERE to AND if it has already been used.
	*queryConcatKeyword = queryConcatKeywordAnd

	return finalizedQuery
}
