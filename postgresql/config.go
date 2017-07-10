package postgresql

import (
	"bytes"
	"database/sql"
	"fmt"
	"log"
	"strings"
	"sync"
	"unicode"

	"github.com/blang/semver"
	"github.com/hashicorp/errwrap"
	_ "github.com/lib/pq" //PostgreSQL db
)

type featureName uint

const (
	featureCreateRoleWith featureName = iota
	featureDBAllowConnections
	featureDBIsTemplate
	featureFallbackApplicationName
	featureRLS
	featureReassignOwnedCurrentUser
	featureSchemaCreateIfNotExist
)

type dbRegistryEntry struct {
	db      *sql.DB
	version semver.Version
}

var (
	dbRegistryLock sync.Mutex
	dbRegistry     map[string]dbRegistryEntry = make(map[string]dbRegistryEntry, 1)

	// Mapping of feature flags to versions
	featureSupported = map[featureName]semver.Range{
		// CREATE ROLE WITH
		featureCreateRoleWith: semver.MustParseRange(">=8.1.0"),

		// CREATE DATABASE has ALLOW_CONNECTIONS support
		featureDBAllowConnections: semver.MustParseRange(">=9.5.0"),

		// CREATE DATABASE has IS_TEMPLATE support
		featureDBIsTemplate: semver.MustParseRange(">=9.5.0"),

		// https://www.postgresql.org/docs/9.0/static/libpq-connect.html
		featureFallbackApplicationName: semver.MustParseRange(">=9.0.0"),

		// CREATE SCHEMA IF NOT EXISTS
		featureSchemaCreateIfNotExist: semver.MustParseRange(">=9.3.0"),

		// REASSIGN OWNED BY { old_role | CURRENT_USER
		featureReassignOwnedCurrentUser: semver.MustParseRange(">=9.5.0"),

		// row-level security
		featureRLS: semver.MustParseRange(">=9.5.0"),
	}
)

// Config - provider config
type Config struct {
	Host              string
	Port              int
	Database          string
	Username          string
	Password          string
	SSLMode           string
	ApplicationName   string
	Timeout           int
	ConnectTimeoutSec int
	MaxConns          int
	ExpectedVersion   semver.Version
}

// Client struct holding connection string
type Client struct {
	// Configuration for the client
	config Config

	// db is a pointer to the DB connection.  Callers are responsible for
	// releasing their connections.
	db *sql.DB

	// version is the version number of the database as determined by parsing the
	// output of `SELECT VERSION()`.x
	version semver.Version

	// PostgreSQL lock on pg_catalog.  Many of the operations that Terraform
	// performs are not permitted to be concurrent.  Unlike traditional
	// PostgreSQL tables that use MVCC, many of the PostgreSQL system
	// catalogs look like tables, but are not in-fact able to be
	// concurrently updated.
	catalogLock sync.RWMutex
}

// NewClient returns new client config
func (c *Config) NewClient() (*Client, error) {
	dbRegistryLock.Lock()
	defer dbRegistryLock.Unlock()

	dsn := c.connStr()
	dbEntry, found := dbRegistry[dsn]
	if !found {
		db, err := sql.Open("postgres", dsn)
		if err != nil {
			return nil, errwrap.Wrapf("Error connecting to PostgreSQL server: {{err}}", err)
		}

		// only one connection
		db.SetMaxIdleConns(1)
		db.SetMaxOpenConns(c.MaxConns)

		version, err := fingerprintCapabilities(db)
		if err != nil {
			db.Close()
			return nil, errwrap.Wrapf("error detecting capabilities: {{err}}", err)
		}

		dbEntry = dbRegistryEntry{
			db:      db,
			version: *version,
		}
		dbRegistry[dsn] = dbEntry
	}

	client := Client{
		config: *c,
		db:     dbEntry.db,
	}

	return &client, nil
}

// featureSupported returns true if a given feature is supported or not.  This
// is slightly different from Client's featureSupported in that here we're
// evaluating against the expected version, not the fingerprinted version.
func (c *Config) featureSupported(name featureName) bool {
	fn, found := featureSupported[name]
	if !found {
		// panic'ing because this is a provider-only bug
		panic(fmt.Sprintf("unknown feature flag %s", name))
	}

	return fn(c.ExpectedVersion)
}

func (c *Config) connStr() string {
	// NOTE: dbname must come before user otherwise dbname will be set to
	// user.
	var dsnFmt string
	{
		dsnFmtParts := []string{
			"host=%s",
			"port=%d",
			"dbname=%s",
			"user=%s",
			"password=%s",
			"sslmode=%s",
			"connect_timeout=%d",
		}

		if c.featureSupported(featureFallbackApplicationName) {
			dsnFmtParts = append(dsnFmtParts, "fallback_application_name=%s")
		}

		dsnFmt = strings.Join(dsnFmtParts, " ")
	}

	// Quote empty strings or strings that contain whitespace
	quote := func(s string) string {
		b := bytes.NewBufferString(`'`)
		b.Grow(len(s) + 2)
		var haveWhitespace bool
		for _, r := range s {
			if unicode.IsSpace(r) {
				haveWhitespace = true
			}

			switch r {
			case '\'':
				b.WriteString(`\'`)
			case '\\':
				b.WriteString(`\\`)
			default:
				b.WriteRune(r)
			}
		}

		b.WriteString(`'`)

		str := b.String()
		if haveWhitespace || len(str) == 2 {
			return str
		}
		return str[1 : len(str)-1]
	}

	{
		logValues := []interface{}{
			quote(c.Host),
			c.Port,
			quote(c.Database),
			quote(c.Username),
			quote("<redacted>"),
			quote(c.SSLMode),
			c.ConnectTimeoutSec,
		}
		if c.featureSupported(featureFallbackApplicationName) {
			logValues = append(logValues, quote(c.ApplicationName))
		}

		logDSN := fmt.Sprintf(dsnFmt, logValues...)
		log.Printf("[INFO] PostgreSQL DSN: `%s`", logDSN)
	}

	var connStr string
	{
		connValues := []interface{}{
			quote(c.Host),
			c.Port,
			quote(c.Database),
			quote(c.Username),
			quote(c.Password),
			quote(c.SSLMode),
			c.ConnectTimeoutSec,
		}
		if c.featureSupported(featureFallbackApplicationName) {
			connValues = append(connValues, quote(c.ApplicationName))
		}
		connStr = fmt.Sprintf(dsnFmt, connValues...)
	}

	return connStr
}

// DB returns a copy to an sql.Open()'ed database connection.  Callers must
// return their database resources.  Use of QueryRow() or Exec() is encouraged.
// Query() must have their rows.Close()'ed.
func (c *Client) DB() *sql.DB {
	return c.db
}

// fingerprintCapabilities queries PostgreSQL to populate a local catalog of
// capabilities.  This is only run once per Client.
func fingerprintCapabilities(db *sql.DB) (*semver.Version, error) {
	var pgVersion string
	err := db.QueryRow(`SELECT VERSION()`).Scan(&pgVersion)
	if err != nil {
		return nil, errwrap.Wrapf("error PostgreSQL version: {{err}}", err)
	}

	// PostgreSQL 9.2.21 on x86_64-apple-darwin16.5.0, compiled by Apple LLVM version 8.1.0 (clang-802.0.42), 64-bit
	fields := strings.Fields(pgVersion)
	if len(fields) < 2 {
		return nil, fmt.Errorf("error determining the server version: %q", pgVersion)
	}

	version, err := semver.Parse(fields[1])
	if err != nil {
		return nil, errwrap.Wrapf("error parsing version: {{err}}", err)
	}

	return &version, nil
}

// featureSupported returns true if a given feature is supported or not. This is
// slightly different from Config's featureSupported in that here we're
// evaluating against the fingerprinted version, not the expected version.
func (c *Client) featureSupported(name featureName) bool {
	fn, found := featureSupported[name]
	if !found {
		// panic'ing because this is a provider-only bug
		panic(fmt.Sprintf("unknown feature flag %s", name))
	}

	return fn(c.version)
}
