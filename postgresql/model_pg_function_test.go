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
		Schema:          "public",
		Name:            "increment",
		Returns:         "integer",
		Language:        "plpgsql",
		Parallel:        defaultFunctionParallel,
		Strict:          false,
		SecurityDefiner: false,
		Volatility:      defaultFunctionVolatility,
		Body:            "BEGIN result = i + 1; END;",
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

func TestFromResourceDataWithArguments(t *testing.T) {
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
		Parallel:        "SAFE",
		Strict:          true,
		SecurityDefiner: true,
		Volatility:      "IMMUTABLE",
	})

	var pgFunction PGFunction

	err := pgFunction.FromResourceData(d)
	if err != nil {
		t.Fatal(err)
	}
	assert.Equal(t, pgFunction, PGFunction{
		Schema:          "public",
		Name:            "increment",
		Returns:         "integer",
		Language:        "plpgsql",
		Parallel:        "SAFE",
		Strict:          true,
		SecurityDefiner: true,
		Volatility:      "IMMUTABLE",
		Body:            "BEGIN result = i + 1; END;",
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
STABLE PARALLEL SAFE STRICT SECURITY DEFINER
AS $function$pg_func_test_body$function$
	`

	var pgFunction PGFunction

	err := pgFunction.Parse(functionDefinition)
	if err != nil {
		t.Fatal(err)
	}

	assert.Equal(t, pgFunction, PGFunction{
		Name:            "pg_func_test",
		Schema:          "public",
		Returns:         "SETOF record",
		Language:        "c",
		Parallel:        "SAFE",
		SecurityDefiner: true,
		Strict:          true,
		Volatility:      "STABLE",
		Body:            "pg_func_test_body",
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
		Name:            "pg_func_test",
		Schema:          "public",
		Returns:         "SETOF record",
		Language:        "plpgsql",
		Parallel:        "UNSAFE",
		SecurityDefiner: false,
		Strict:          false,
		Volatility:      "VOLATILE",
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
	attributes["strict"] = obj.Strict
	attributes["security_definer"] = obj.SecurityDefiner
	attributes["parallel"] = obj.Parallel
	attributes["volatility"] = obj.Volatility

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
