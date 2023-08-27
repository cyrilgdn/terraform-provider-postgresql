package postgresql

import (
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFindStringSubmatchMap(t *testing.T) {

	resultMap := findStringSubmatchMap(`(?si).*\$(?P<Body>.*)\$.*`, "aa $somehing_to_extract$ bb")

	assert.Equal(t,
		resultMap,
		map[string]string{
			"Body": "somehing_to_extract",
		},
	)
}

func TestArePrivilegesEqual(t *testing.T) {

	type PrivilegesTestObject struct {
		d         *schema.ResourceData
		granted   *schema.Set
		wanted    *schema.Set
		assertion bool
	}

	tt := []PrivilegesTestObject{
		{
			buildResourceData("database", t),
			buildPrivilegesSet("CONNECT", "CREATE", "TEMPORARY"),
			all(),
			true,
		},
		{
			buildResourceData("database", t),
			buildPrivilegesSet("CREATE", "USAGE"),
			buildPrivilegesSet("USAGE"),
			false,
		},
		{
			buildResourceData("table", t),
			buildPrivilegesSet("SELECT", "INSERT", "UPDATE", "DELETE", "TRUNCATE", "REFERENCES", "TRIGGER"),
			all(),
			true,
		},
		{
			buildResourceData("table", t),
			buildPrivilegesSet("SELECT"),
			buildPrivilegesSet("SELECT, INSERT"),
			false,
		},
		{
			buildResourceData("schema", t),
			buildPrivilegesSet("CREATE", "USAGE"),
			all(),
			true,
		},
		{
			buildResourceData("schema", t),
			buildPrivilegesSet("CREATE"),
			all(),
			false,
		},
	}

	for _, configuration := range tt {
		equal := arePrivilegesEqual(configuration.granted, configuration.wanted, configuration.d)
		assert.Equal(t, configuration.assertion, equal)
	}
}

func buildPrivilegesSet(grants ...interface{}) *schema.Set {
	return schema.NewSet(schema.HashString, grants)
}

func all() *schema.Set {
	return buildPrivilegesSet("ALL")
}

func buildResourceData(objectType string, t *testing.T) *schema.ResourceData {
	var testSchema = map[string]*schema.Schema{
		"object_type": {Type: schema.TypeString},
	}
	m := make(map[string]interface{})
	m["object_type"] = objectType
	return schema.TestResourceDataRaw(t, testSchema, m)
}
