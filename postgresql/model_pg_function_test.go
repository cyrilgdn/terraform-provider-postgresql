package postgresql

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
	"github.com/stretchr/testify/assert"
)

func TestFromResourceData(t *testing.T) {
	d := mockFunctionResourceData(t, PGFunction{
		Name: "increment",
		Body: "BEGIN result = i + 1; END;",
		Args: []PGFunctionArg{
			{
				Name: "i",
				Type: "integer",
			},
			{
				Name: "result",
				Type: "integer",
				Mode: "OUT",
			},
		},
	})

	var pgFunction PGFunction

	err := pgFunction.FromResourceData(d)
	if err != nil {
		t.Fatal(err)
	}
	assert.Equal(t, pgFunction, PGFunction{
		Schema:   "public",
		Name:     "increment",
		Returns:  "integer",
		Language: "plpgsql",
		Body:     "BEGIN result = i + 1; END;",
		Args: []PGFunctionArg{
			{
				Name: "i",
				Type: "integer",
			},
			{
				Name: "result",
				Type: "integer",
				Mode: "OUT",
			},
		},
	})
}

func TestPGFunctionParseWithArguments(t *testing.T) {

	var functionDefinition = `
CREATE OR REPLACE FUNCTION public.pg_func_test(showtext boolean, OUT userid oid, default_null integer DEFAULT NULL::integer, simple_default integer DEFAULT 42, long_default character varying DEFAULT 'foo'::character varying)
RETURNS SETOF record
LANGUAGE c
PARALLEL SAFE STRICT
AS $function$pg_func_test_body$function$
	`

	var pgFunction PGFunction

	err := pgFunction.Parse(functionDefinition)
	if err != nil {
		t.Fatal(err)
	}

	assert.Equal(t, pgFunction, PGFunction{
		Name:     "pg_func_test",
		Schema:   "public",
		Returns:  "SETOF record",
		Language: "c",
		Body:     "pg_func_test_body",
		Args: []PGFunctionArg{
			{
				Mode: "IN",
				Name: "showtext",
				Type: "boolean",
			},
			{
				Mode: "OUT",
				Name: "userid",
				Type: "oid",
			},
			{
				Mode:    "IN",
				Name:    "default_null",
				Type:    "integer",
				Default: "NULL::integer",
			},
			{
				Mode:    "IN",
				Name:    "simple_default",
				Type:    "integer",
				Default: "42",
			},
			{
				Mode:    "IN",
				Name:    "long_default",
				Type:    "character varying",
				Default: "'foo'::character varying",
			},
		},
	})
}

func TestPGFunctionParseWithoutArguments(t *testing.T) {

	var functionDefinition = `
CREATE OR REPLACE FUNCTION public.pg_func_test()
RETURNS SETOF record
LANGUAGE plpgsql
PARALLEL SAFE STRICT
AS $function$
MultiLine Function
$function$
	`

	var pgFunction PGFunction

	err := pgFunction.Parse(functionDefinition)
	if err != nil {
		t.Fatal(err)
	}

	assert.Equal(t, pgFunction, PGFunction{
		Name:     "pg_func_test",
		Schema:   "public",
		Returns:  "SETOF record",
		Language: "plpgsql",
		Body: `
MultiLine Function
`,
		Args: []PGFunctionArg{},
	})
}

func TestPGFunctionArgParseWithDefault(t *testing.T) {

	var functionArgDefinition = `default_null integer DEFAULT NULL::integer`

	var pgFunctionArg PGFunctionArg

	err := pgFunctionArg.Parse(functionArgDefinition)
	if err != nil {
		t.Fatal(err)
	}

	assert.Equal(t, pgFunctionArg, PGFunctionArg{
		Mode:    "IN",
		Name:    "default_null",
		Type:    "integer",
		Default: "NULL::integer",
	})
}

func TestPGFunctionArgParseWithoutDefault(t *testing.T) {

	var functionArgDefinition = `num integer`

	var pgFunctionArg PGFunctionArg

	err := pgFunctionArg.Parse(functionArgDefinition)
	if err != nil {
		t.Fatal(err)
	}

	assert.Equal(t, pgFunctionArg, PGFunctionArg{
		Mode: "IN",
		Name: "num",
		Type: "integer",
	})
}

func mockFunctionResourceData(t *testing.T, obj PGFunction) *schema.ResourceData {

	state := terraform.InstanceState{}

	state.ID = ""
	// Build the attribute map from ForemanModel
	attributes := make(map[string]interface{})

	attributes["name"] = obj.Name
	attributes["returns"] = obj.Returns
	attributes["language"] = obj.Language
	attributes["body"] = obj.Body

	var args []interface{}

	for _, a := range obj.Args {
		args = append(args, map[string]interface{}{
			"type":    a.Type,
			"name":    a.Name,
			"mode":    a.Mode,
			"default": a.Default,
		})
	}

	attributes["arg"] = args

	return schema.TestResourceDataRaw(t, resourcePostgreSQLFunction().Schema, attributes)
}
