package postgresql

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
	"github.com/stretchr/testify/assert"
)

func TestProcFromResourceData(t *testing.T) {
	d := mockProcedureResourceData(t, PGProcedure{
		Name: "increment",
		Body: "BEGIN result = i + 1; END;",
		Args: []PGProcedureArg{
			{
				Name: "i",
				Type: "integer",
			},
		},
	})

	var pgProcedure PGProcedure

	err := pgProcedure.FromResourceData(d)
	if err != nil {
		t.Fatal(err)
	}
	assert.Equal(t, pgProcedure, PGProcedure{
		Schema:          "public",
		Name:            "increment",
		Language:        "plpgsql",
		SecurityDefiner: false,
		Body:            "BEGIN result = i + 1; END;",
		Args: []PGProcedureArg{
			{
				Name: "i",
				Type: "integer",
			},
		},
	})
}

func TestProcFromResourceDataWithArguments(t *testing.T) {
	d := mockProcedureResourceData(t, PGProcedure{
		Name: "increment",
		Body: "BEGIN result = i + 1; END;",
		Args: []PGProcedureArg{
			{
				Name: "i",
				Type: "integer",
			},
		},
		SecurityDefiner: true,
	})

	var pgFunction PGProcedure

	err := pgFunction.FromResourceData(d)
	if err != nil {
		t.Fatal(err)
	}
	assert.Equal(t, pgFunction, PGProcedure{
		Schema:          "public",
		Name:            "increment",
		Language:        "plpgsql",
		SecurityDefiner: true,
		Body:            "BEGIN result = i + 1; END;",
		Args: []PGProcedureArg{
			{
				Name: "i",
				Type: "integer",
			},
		},
	})
}

func TestPGProcedureParseWithArguments(t *testing.T) {

	var functionDefinition = `
CREATE OR REPLACE PROCEDURE public.pg_func_test(showtext boolean, default_null integer DEFAULT NULL::integer, simple_default integer DEFAULT 42, long_default character varying DEFAULT 'foo'::character varying)
LANGUAGE sql
SECURITY DEFINER
AS $function$SELECT 1;$function$
`

	var pgFunction PGProcedure

	err := pgFunction.Parse(functionDefinition)
	if err != nil {
		t.Fatal(err)
	}

	assert.Equal(t, pgFunction, PGProcedure{
		Name:            "pg_func_test",
		Schema:          "public",
		Language:        "sql",
		SecurityDefiner: true,
		Body:            "SELECT 1;",
		Args: []PGProcedureArg{
			{
				Mode: "IN",
				Name: "showtext",
				Type: "boolean",
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

func TestPGProcedureParseWithoutArguments(t *testing.T) {

	var functionDefinition = `
CREATE OR REPLACE PROCEDURE public.pg_func_test()
LANGUAGE plpgsql
AS $function$
MultiLine Function
$function$
	`

	var pgFunction PGProcedure

	err := pgFunction.Parse(functionDefinition)
	if err != nil {
		t.Fatal(err)
	}

	assert.Equal(t, pgFunction, PGProcedure{
		Name:            "pg_func_test",
		Schema:          "public",
		Language:        "plpgsql",
		SecurityDefiner: false,
		Body: `
MultiLine Function
`,
		Args: []PGProcedureArg{},
	})
}

func TestPGProcedureArgParseWithDefault(t *testing.T) {

	var functionArgDefinition = `default_null integer DEFAULT NULL::integer`

	var pgProcedureArg PGProcedureArg

	err := pgProcedureArg.Parse(functionArgDefinition)
	if err != nil {
		t.Fatal(err)
	}

	assert.Equal(t, pgProcedureArg, PGProcedureArg{
		Mode:    "IN",
		Name:    "default_null",
		Type:    "integer",
		Default: "NULL::integer",
	})
}

func TestPGProcedureArgParseWithoutDefault(t *testing.T) {

	var functionArgDefinition = `num integer`

	var pgFunctionArg PGProcedureArg

	err := pgFunctionArg.Parse(functionArgDefinition)
	if err != nil {
		t.Fatal(err)
	}

	assert.Equal(t, pgFunctionArg, PGProcedureArg{
		Mode: "IN",
		Name: "num",
		Type: "integer",
	})
}

func mockProcedureResourceData(t *testing.T, obj PGProcedure) *schema.ResourceData {

	state := terraform.InstanceState{}

	state.ID = ""
	// Build the attribute map from ForemanModel
	attributes := make(map[string]interface{})

	attributes["name"] = obj.Name
	attributes["language"] = obj.Language
	attributes["body"] = obj.Body
	attributes["security_definer"] = obj.SecurityDefiner

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

	return schema.TestResourceDataRaw(t, resourcePostgreSQLProcedure().Schema, attributes)
}
