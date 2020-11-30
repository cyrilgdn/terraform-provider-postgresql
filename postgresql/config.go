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
	_ "github.com/lib/pq" //PostgreSQL db
)

type featureName uint

const (
	featureCreateRoleWith featureName = iota
	featureDBAllowConnections
	featureDBIsTemplate
	featureFallbackApplicationName
	featureRLS
	featureSchemaCreateIfNotExist
	featureReplication
	featureExtension
	featurePrivileges
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

		// row-level security
		featureRLS: semver.MustParseRange(">=9.5.0"),

		// CREATE ROLE has REPLICATION support.
		featureReplication: semver.MustParseRange(">=9.1.0"),

		// CREATE EXTENSION support.
		featureExtension: semver.MustParseRange(">=9.1.0"),

		// We do not support postgresql_grant and postgresql_default_privileges
		// for Postgresql < 9.
		featurePrivileges: semver.MustParseRange(">=9.0.0"),
	}
)

type ClientCertificateConfig struct {
	CertificatePath string
	KeyPath         string
}

// Config - provider config
type Config struct {
	Host              string
	Port              int
	Username          string
	Password          string
	DatabaseUsername  string
	Superuser         bool
	SSLMode           string
	ApplicationName   string
	Timeout           int
	ConnectTimeoutSec int
	MaxConns          int
	ExpectedVersion   semver.Version
	SSLClientCert     *ClientCertificateConfig
	SSLRootCertPath   string
}

// Client struct holding connection string
type Client struct {
	// Configuration for the client
	config Config

	databaseName string

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

// NewClient returns client config for the specified database.
func (c *Config) NewClient(database string) (*Client, error) {
	dbRegistryLock.Lock()
	defer dbRegistryLock.Unlock()

	dsn := c.connStr(database)
	dbEntry, found := dbRegistry[dsn]
	if !found {
		db, err := sql.Open("postgres", dsn)
		if err != nil {
			return nil, fmt.Errorf("Error connecting to PostgreSQL server: %w", err)
		}

		// We don't want to retain connection
		// So when we connect on a specific database which might be managed by terraform,
		// we don't keep opened connection in case of the db has to be dopped in the plan.
		db.SetMaxIdleConns(0)
		db.SetMaxOpenConns(c.MaxConns)

		defaultVersion, _ := semver.Parse(defaultExpectedPostgreSQLVersion)
		version := &c.ExpectedVersion
		if defaultVersion.Equals(c.ExpectedVersion) {
			// Version hint not set by user, need to fingerprint
			version, err = fingerprintCapabilities(db)
			if err != nil {
				db.Close()
				return nil, fmt.Errorf("error detecting capabilities: %w", err)
			}
		}

		dbEntry = dbRegistryEntry{
			db:      db,
			version: *version,
		}
		dbRegistry[dsn] = dbEntry
	}

	client := Client{
		config:       *c,
		databaseName: database,
		db:           dbEntry.db,
		version:      dbEntry.version,
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
		panic(fmt.Sprintf("unknown feature flag %v", name))
	}

	return fn(c.ExpectedVersion)
}

func (c *Config) connStr(database string) string {
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
		if c.SSLClientCert != nil {
			dsnFmtParts = append(
				dsnFmtParts,
				"sslcert=%s",
				"sslkey=%s",
			)
		}
		if c.SSLRootCertPath != "" {
			dsnFmtParts = append(dsnFmtParts, "sslrootcert=%s")
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
			quote(database),
			quote(c.Username),
			quote("<redacted>"),
			quote(c.SSLMode),
			c.ConnectTimeoutSec,
		}
		if c.featureSupported(featureFallbackApplicationName) {
			logValues = append(logValues, quote(c.ApplicationName))
		}
		if c.SSLClientCert != nil {
			logValues = append(
				logValues,
				quote(c.SSLClientCert.CertificatePath),
				quote(c.SSLClientCert.KeyPath),
			)
		}
		if c.SSLRootCertPath != "" {
			logValues = append(logValues, quote(c.SSLRootCertPath))
		}

		logDSN := fmt.Sprintf(dsnFmt, logValues...)
		log.Printf("[INFO] PostgreSQL DSN: `%s`", logDSN)
	}

	var connStr string
	{
		connValues := []interface{}{
			quote(c.Host),
			c.Port,
			quote(database),
			quote(c.Username),
			quote(c.Password),
			quote(c.SSLMode),
			c.ConnectTimeoutSec,
		}
		if c.featureSupported(featureFallbackApplicationName) {
			connValues = append(connValues, quote(c.ApplicationName))
		}
		if c.SSLClientCert != nil {
			connValues = append(
				connValues,
				quote(c.SSLClientCert.CertificatePath),
				quote(c.SSLClientCert.KeyPath),
			)
		}
		if c.SSLRootCertPath != "" {
			connValues = append(connValues, quote(c.SSLRootCertPath))
		}

		connStr = fmt.Sprintf(dsnFmt, connValues...)
	}

	return connStr
}

func (c *Config) getDatabaseUsername() string {
	if c.DatabaseUsername != "" {
		return c.DatabaseUsername
	}
	return c.Username
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
		return nil, fmt.Errorf("error PostgreSQL version: %w", err)
	}

	// PostgreSQL 9.2.21 on x86_64-apple-darwin16.5.0, compiled by Apple LLVM version 8.1.0 (clang-802.0.42), 64-bit
	// PostgreSQL 9.6.7, compiled by Visual C++ build 1800, 64-bit
	fields := strings.FieldsFunc(pgVersion, func(c rune) bool {
		return unicode.IsSpace(c) || c == ','
	})
	if len(fields) < 2 {
		return nil, fmt.Errorf("error determining the server version: %q", pgVersion)
	}

	version, err := semver.ParseTolerant(fields[1])
	if err != nil {
		return nil, fmt.Errorf("error parsing version: %w", err)
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
		panic(fmt.Sprintf("unknown feature flag %v", name))
	}

	return fn(c.version)
}

// isSuperuser returns true if connected user is a Postgres SUPERUSER
func (c *Client) isSuperuser() (bool, error) {
	var superuser bool

	if err := c.db.QueryRow("SELECT rolsuper FROM pg_roles WHERE rolname = CURRENT_USER").Scan(&superuser); err != nil {
		return false, fmt.Errorf("could not check if current user is superuser: %w", err)
	}

	return superuser, nil
}
