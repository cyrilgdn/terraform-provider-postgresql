package postgresql

import (
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFindStringSubmatchMap(t *testing.T) {

	resultMap := findStringSubmatchMap(`(?si).*\$(?P<Body>.*)\$.*`, "aa $something_to_extract$ bb")

	assert.Equal(t,
		resultMap,
		map[string]string{
			"Body": "something_to_extract",
		},
	)
}

func TestQuoteTableName(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "simple table name",
			input:    "users",
			expected: `"users"`,
		},
		{
			name:     "table name with schema",
			input:    "test.users",
			expected: `"test"."users"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual := quoteTableName(tt.input)
			if actual != tt.expected {
				t.Errorf("quoteTableName() = %v, want %v", actual, tt.expected)
			}
		})
	}
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
			buildPrivilegesSet("ALL"),
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
			buildPrivilegesSet("SELECT", "INSERT", "UPDATE", "DELETE", "TRUNCATE", "REFERENCES", "TRIGGER", "MAINTAIN"),
			buildPrivilegesSet("ALL"),
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
			buildPrivilegesSet("ALL"),
			true,
		},
		{
			buildResourceData("schema", t),
			buildPrivilegesSet("CREATE"),
			buildPrivilegesSet("ALL"),
			false,
		},
	}

	for _, configuration := range tt {
		err := configuration.d.Set("privileges", configuration.wanted)
		assert.NoError(t, err)
		equal := resourcePrivilegesEqual(configuration.granted, configuration.d)
		assert.Equal(t, configuration.assertion, equal)
	}
}

func buildPrivilegesSet(grants ...interface{}) *schema.Set {
	return schema.NewSet(schema.HashString, grants)
}

func buildResourceData(objectType string, t *testing.T) *schema.ResourceData {
	var testSchema = map[string]*schema.Schema{
		"object_type": {Type: schema.TypeString},
		"privileges": {
			Type: schema.TypeSet,
			Elem: &schema.Schema{Type: schema.TypeString},
			Set:  schema.HashString,
		},
	}

	m := make(map[string]any)
	m["object_type"] = objectType
	return schema.TestResourceDataRaw(t, testSchema, m)
}
