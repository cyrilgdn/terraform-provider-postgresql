package postgresql

import (
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"net"
	"syscall"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/lib/pq"
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
			buildPrivilegesSet("SELECT", "INSERT", "UPDATE", "DELETE", "TRUNCATE", "REFERENCES", "TRIGGER"),
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

func buildPrivilegesSet(grants ...any) *schema.Set {
	return schema.NewSet(schema.HashString, grants)
}

func TestIsTransientConnErr(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"driver.ErrBadConn", driver.ErrBadConn, true},
		{"wrapped driver.ErrBadConn", fmt.Errorf("wrap: %w", driver.ErrBadConn), true},
		{"io.EOF", io.EOF, true},
		{"net.OpError ECONNRESET", &net.OpError{Op: "read", Err: syscall.ECONNRESET}, true},
		{"wrapped net.OpError", fmt.Errorf("could not start transaction: %w", &net.OpError{Op: "read", Err: syscall.ECONNRESET}), true},
		{"syscall.ECONNRESET", syscall.ECONNRESET, true},
		{"syscall.EPIPE", syscall.EPIPE, true},
		{"pq error 08006", &pq.Error{Code: "08006"}, true},
		{"pq error 08003", &pq.Error{Code: "08003"}, true},
		{"pq error 57P01", &pq.Error{Code: "57P01"}, true},
		{"pq error 23505 (unique violation)", &pq.Error{Code: "23505"}, false},
		{"substring connection reset by peer", errors.New("read tcp 1.2.3.4:5432->5.6.7.8:6432: read: connection reset by peer"), true},
		{"substring broken pipe", errors.New("write: broken pipe"), true},
		{"random error", errors.New("syntax error at or near \"FOO\""), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, isTransientConnErr(tc.err))
		})
	}
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
