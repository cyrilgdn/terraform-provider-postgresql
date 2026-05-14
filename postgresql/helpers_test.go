package postgresql

import (
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"net"
	"syscall"
	"testing"
	"time"

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

// Regression: prior to the fix, the scheduler slot was keyed by
// client.databaseName (the maintenance database, usually shared across all
// resources in a provider config). Resources like postgresql_grant target
// a *different* database via d.Get("database"), so cap=1 must serialize
// them per target, not per maintenance.
func TestTargetDatabaseFromAttr(t *testing.T) {
	dbAttrSchema := map[string]*schema.Schema{
		"database": {Type: schema.TypeString},
		"name":     {Type: schema.TypeString},
	}
	client := &Client{databaseName: "postgres"}

	dA := schema.TestResourceDataRaw(t, dbAttrSchema, map[string]any{"database": "app_a"})
	dB := schema.TestResourceDataRaw(t, dbAttrSchema, map[string]any{"database": "app_b"})

	assert.Equal(t, "app_a", targetDatabaseFromAttr(dA, client, "database"))
	assert.Equal(t, "app_b", targetDatabaseFromAttr(dB, client, "database"))

	// Empty value falls back to client.databaseName.
	dEmpty := schema.TestResourceDataRaw(t, dbAttrSchema, map[string]any{"database": ""})
	assert.Equal(t, "postgres", targetDatabaseFromAttr(dEmpty, client, "database"))

	// postgresql_database-like extractor: target DB lives under "name".
	dNamed := schema.TestResourceDataRaw(t, dbAttrSchema, map[string]any{"name": "managed_x"})
	assert.Equal(t, "managed_x", targetDatabaseFromAttr(dNamed, client, "name"))

	// Schema without the requested attribute falls back to maintenance.
	noAttrSchema := map[string]*schema.Schema{"other": {Type: schema.TypeString}}
	dNoAttr := schema.TestResourceDataRaw(t, noAttrSchema, map[string]any{"other": "x"})
	assert.Equal(t, "postgres", targetDatabaseFromAttr(dNoAttr, client, "database"))

	// attr=="" always returns maintenance.
	assert.Equal(t, "postgres", targetDatabaseFromAttr(dA, client, ""))
}

// End-to-end: two ResourceData with different `database` values, sharing the
// same meta-client whose databaseName is the maintenance DB. With cap=1, the
// second resource MUST block until the first releases — even though they
// both go through the same Client value (because the slot is keyed by
// target, not by client.databaseName).
func TestPGResourceFunc_SerializesByTargetDatabase(t *testing.T) {
	dbAttrSchema := map[string]*schema.Schema{
		"database": {Type: schema.TypeString},
	}
	cfg := &Config{MaxConcurrentDatabases: 1}
	cfg.ensureInit()
	client := &Client{config: *cfg, databaseName: "postgres"}

	dA := schema.TestResourceDataRaw(t, dbAttrSchema, map[string]any{"database": "app_a"})
	dB := schema.TestResourceDataRaw(t, dbAttrSchema, map[string]any{"database": "app_b"})

	startedA := make(chan struct{})
	releaseA := make(chan struct{})
	fnA := func(d *schema.ResourceData, meta any) error {
		c := meta.(*Client)
		release, err := acquireDBSlot(c, targetDatabaseFromAttr(d, c, "database"))
		if err != nil {
			return err
		}
		defer release()
		close(startedA)
		<-releaseA
		return nil
	}

	startedB := make(chan struct{})
	fnB := func(d *schema.ResourceData, meta any) error {
		c := meta.(*Client)
		release, err := acquireDBSlot(c, targetDatabaseFromAttr(d, c, "database"))
		if err != nil {
			return err
		}
		defer release()
		close(startedB)
		return nil
	}

	go fnA(dA, client) //nolint:errcheck
	<-startedA

	go fnB(dB, client) //nolint:errcheck

	select {
	case <-startedB:
		t.Fatal("app_b started while app_a was holding the only slot")
	case <-time.After(50 * time.Millisecond):
	}

	close(releaseA)
	select {
	case <-startedB:
	case <-time.After(time.Second):
		t.Fatal("app_b was not granted after app_a released")
	}
}

// Regression: postgresql_database has no "database" attribute — its target is
// d.Get("name"). With the generic wrapper this would refcount on
// client.databaseName (maintenance), letting two databases through in
// parallel. PGResourceFuncForDB("name", ...) keys by name and serializes them.
func TestAcquireDBSlot_SerializesByNameAttr(t *testing.T) {
	nameSchema := map[string]*schema.Schema{
		"name": {Type: schema.TypeString},
	}
	cfg := &Config{MaxConcurrentDatabases: 1}
	cfg.ensureInit()
	client := &Client{config: *cfg, databaseName: "postgres"}

	dA := schema.TestResourceDataRaw(t, nameSchema, map[string]any{"name": "managed_a"})
	dB := schema.TestResourceDataRaw(t, nameSchema, map[string]any{"name": "managed_b"})

	// With the wrong key ("database"), both fall back to maintenance and the
	// scheduler lets them through reentrantly — the bug.
	keyA := targetDatabaseFromAttr(dA, client, "database")
	keyB := targetDatabaseFromAttr(dB, client, "database")
	assert.Equal(t, keyA, keyB, "wrong key collapses to maintenance for both")

	// With the right key ("name"), they get distinct keys and serialize.
	assert.Equal(t, "managed_a", targetDatabaseFromAttr(dA, client, "name"))
	assert.Equal(t, "managed_b", targetDatabaseFromAttr(dB, client, "name"))

	startedB := make(chan struct{})
	releaseA := make(chan struct{})

	relA, err := acquireDBSlot(client, targetDatabaseFromAttr(dA, client, "name"))
	assert.NoError(t, err)

	go func() {
		relB, err := acquireDBSlot(client, targetDatabaseFromAttr(dB, client, "name"))
		if err != nil {
			t.Errorf("acquire B: %v", err)
			return
		}
		defer relB()
		close(startedB)
	}()

	select {
	case <-startedB:
		t.Fatal("managed_b started while managed_a was holding the only slot")
	case <-time.After(50 * time.Millisecond):
	}

	relA()
	close(releaseA)
	select {
	case <-startedB:
	case <-time.After(time.Second):
		t.Fatal("managed_b was not granted after managed_a released")
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
