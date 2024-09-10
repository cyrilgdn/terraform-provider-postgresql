package postgresql

import (
	"strings"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
)

// PGProcedure is the model for the database procedure
type PGProcedure struct {
	Schema          string
	Name            string
	Language        string
	Body            string
	Args            []PGProcedureArg
	SecurityDefiner bool
}

type PGProcedureArg struct {
	Name    string
	Type    string
	Mode    string
	Default string
}

func (pgProcedure *PGProcedure) FromResourceData(d *schema.ResourceData) error {

	if v, ok := d.GetOk(funcSchemaAttr); ok {
		pgProcedure.Schema = v.(string)
	} else {
		pgProcedure.Schema = "public"
	}

	pgProcedure.Name = d.Get(funcNameAttr).(string)
	if v, ok := d.GetOk(funcLanguageAttr); ok {
		pgProcedure.Language = v.(string)
	} else {
		pgProcedure.Language = "plpgsql"
	}
	pgProcedure.Body = normalizeFunctionBody(d.Get(funcBodyAttr).(string))
	pgProcedure.Args = []PGProcedureArg{}

	if v, ok := d.GetOk(funcSecurityDefinerAttr); ok {
		pgProcedure.SecurityDefiner = v.(bool)
	} else {
		pgProcedure.SecurityDefiner = false
	}

	if args, ok := d.GetOk(funcArgAttr); ok {
		args := args.([]interface{})

		for _, arg := range args {
			arg := arg.(map[string]interface{})

			var pgArg PGProcedureArg

			if v, ok := arg[funcArgModeAttr]; ok {
				pgArg.Mode = v.(string)
			}

			if v, ok := arg[funcArgNameAttr]; ok {
				pgArg.Name = v.(string)
			}

			pgArg.Type = arg[funcArgTypeAttr].(string)

			if v, ok := arg[funcArgDefaultAttr]; ok {
				pgArg.Default = v.(string)
			}

			pgProcedure.Args = append(pgProcedure.Args, pgArg)
		}
	}

	return nil
}

func (pgProcedure *PGProcedure) Parse(functionDefinition string) error {

	pgProcedureData := findStringSubmatchMap(
		`(?si)CREATE\sOR\sREPLACE\sPROCEDURE\s(?P<Schema>[^.]+)\.(?P<Name>[^(]+)\((?P<Args>.*)\).*LANGUAGE\s(?P<Language>[^\n\s]+)\s*(?P<Security>(SECURITY DEFINER)?).*\$[a-zA-Z]*\$(?P<Body>.*)\$[a-zA-Z]*\$`,
		functionDefinition,
	)

	argsData := pgProcedureData["Args"]

	args := []PGProcedureArg{}

	if argsData != "" {
		rawArgs := strings.Split(argsData, ",")
		for i := 0; i < len(rawArgs); i++ {
			var arg PGProcedureArg
			err := arg.Parse(rawArgs[i])
			if err != nil {
				continue
			}
			args = append(args, arg)
		}
	}

	pgProcedure.Schema = pgProcedureData["Schema"]
	pgProcedure.Name = pgProcedureData["Name"]
	pgProcedure.Language = pgProcedureData["Language"]
	pgProcedure.Body = pgProcedureData["Body"]
	pgProcedure.Args = args
	pgProcedure.SecurityDefiner = len(pgProcedureData["Security"]) > 0

	return nil
}

func (pgProcedureArg *PGProcedureArg) Parse(ProcedureArgDefinition string) error {

	// Check if default exists
	argDefinitions := findStringSubmatchMap(`(?si)(?P<ArgData>.*)\sDEFAULT\s(?P<ArgDefault>.*)`, ProcedureArgDefinition)

	argData := ProcedureArgDefinition
	if len(argDefinitions) > 0 {
		argData = argDefinitions["ArgData"]
		pgProcedureArg.Default = argDefinitions["ArgDefault"]
	}

	pgProcedureArgData := findStringSubmatchMap(`(?si)((?P<Mode>IN|OUT|INOUT|VARIADIC)\s)?(?P<Name>[^\s]+)\s(?P<Type>.*)`, argData)

	pgProcedureArg.Name = pgProcedureArgData["Name"]
	pgProcedureArg.Type = pgProcedureArgData["Type"]
	pgProcedureArg.Mode = pgProcedureArgData["Mode"]
	if pgProcedureArg.Mode == "" {
		pgProcedureArg.Mode = "IN"
	}
	return nil
}

func normalizeProcedureBody(body string) string {
	newBodyMap := findStringSubmatchMap(`(?si).*\$[a-zA-Z]*\$\s(?P<Body>.*)\s\$[a-zA-Z]*\$.*`, body)
	if newBody, ok := newBodyMap["Body"]; ok {
		return newBody
	}
	return body
}
