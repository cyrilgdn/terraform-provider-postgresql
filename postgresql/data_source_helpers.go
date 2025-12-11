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

func applyPatternMatchingToQuery(patternMatchingTarget string, d *schema.ResourceData) []string {
	likeAnyPatterns := d.Get("like_any_patterns").([]any)
	likeAllPatterns := d.Get("like_all_patterns").([]any)
	notLikeAllPatterns := d.Get("not_like_all_patterns").([]any)
	regexPattern := d.Get("regex_pattern").(string)

	filters := []string{}
	if len(likeAnyPatterns) > 0 {
		filters = append(filters, generatePatternMatchingString(patternMatchingTarget, likePatternQuery, generatePatternArrayString(likeAnyPatterns, queryArrayKeywordAny)))
	}
	if len(likeAllPatterns) > 0 {
		filters = append(filters, generatePatternMatchingString(patternMatchingTarget, likePatternQuery, generatePatternArrayString(likeAllPatterns, queryArrayKeywordAll)))
	}
	if len(notLikeAllPatterns) > 0 {
		filters = append(filters, generatePatternMatchingString(patternMatchingTarget, notLikePatternQuery, generatePatternArrayString(notLikeAllPatterns, queryArrayKeywordAll)))
	}
	if regexPattern != "" {
		filters = append(filters, generatePatternMatchingString(patternMatchingTarget, regexPatternQuery, fmt.Sprintf("'%s'", regexPattern)))
	}

	return filters
}

func generatePatternMatchingString(patternMatchingTarget string, additionalQueryKeyword string, pattern string) string {
	patternMatchingFilter := fmt.Sprintf("%s %s %s", patternMatchingTarget, additionalQueryKeyword, pattern)

	return patternMatchingFilter
}

func applyTypeMatchingToQuery(objectKeyword string, objects []any) string {
	var typeFilter string
	if len(objects) > 0 {
		typeFilter = fmt.Sprintf("%s = %s", objectKeyword, generatePatternArrayString(objects, queryArrayKeywordAny))
	}

	return typeFilter
}

func generatePatternArrayString(patterns []any, queryArrayKeyword string) string {
	formattedPatterns := []string{}

	for _, pattern := range patterns {
		formattedPatterns = append(formattedPatterns, fmt.Sprintf("'%s'", pattern.(string)))
	}
	return fmt.Sprintf("%s (array[%s])", queryArrayKeyword, strings.Join(formattedPatterns, ","))
}

func finalizeQueryWithFilters(query string, queryConcatKeyword string, filters []string) string {
	if len(filters) > 0 {
		query = fmt.Sprintf("%s %s %s", query, queryConcatKeyword, strings.Join(filters, " AND "))
	}

	return query
}
