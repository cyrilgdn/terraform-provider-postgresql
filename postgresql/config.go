package postgresql

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/blang/semver"
	_ "github.com/lib/pq" // PostgreSQL db
	"gocloud.dev/gcp"
	"gocloud.dev/gcp/cloudsql"
	"gocloud.dev/postgres"
	_ "gocloud.dev/postgres/awspostgres"
	"gocloud.dev/postgres/gcppostgres"
	"google.golang.org/api/impersonate"
)

type featureName uint

const (
	featureCreateRoleWith featureName = iota
	featureDatabaseOwnerRole
	featureDBAllowConnections
	featureDBIsTemplate
	featureFallbackApplicationName
	featureRLS
	featureSchemaCreateIfNotExist
	featureReplication
	featureExtension
	featurePrivileges
	featureProcedure
	featureRoutine
	featurePrivilegesOnSchemas
	featureForceDropDatabase
	featurePid
	featurePublishViaRoot
	featurePubTruncate
	featurePublication
	featurePubWithoutTruncate
	featureFunction
	featureServer
	featureCreateRoleSelfGrant
	featureSecurityLabel
)

type registryEntry struct {
	once sync.Once
	conn *DBConnection
	err  error
}

var (
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

		// Object PROCEDURE support
		featureProcedure: semver.MustParseRange(">=11.0.0"),

		// Object ROUTINE support
		featureRoutine: semver.MustParseRange(">=11.0.0"),
		// ALTER DEFAULT PRIVILEGES has ON SCHEMAS support
		// for Postgresql >= 10
		featurePrivilegesOnSchemas: semver.MustParseRange(">=10.0.0"),

		// DROP DATABASE WITH FORCE
		// for Postgresql >= 13
		featureForceDropDatabase: semver.MustParseRange(">=13.0.0"),

		// Column procpid was replaced by pid in pg_stat_activity
		// for Postgresql >= 9.2 and above
		featurePid: semver.MustParseRange(">=9.2.0"),

		// attribute publish_via_partition_root for partition is supported
		featurePublishViaRoot: semver.MustParseRange(">=13.0.0"),

		// attribute pubtruncate for publications is supported
		featurePubTruncate: semver.MustParseRange(">=11.0.0"),

		// attribute pubtruncate for publications is supported
		featurePubWithoutTruncate: semver.MustParseRange("<11.0.0"),

		// publication is Supported
		featurePublication: semver.MustParseRange(">=10.0.0"),

		// We do not support CREATE FUNCTION for Postgresql < 8.4
		featureFunction: semver.MustParseRange(">=8.4.0"),
		// CREATE SERVER support
		featureServer: semver.MustParseRange(">=10.0.0"),

		featureDatabaseOwnerRole: semver.MustParseRange(">=15.0.0"),

		// New privileges rules in version 16
		// https://www.postgresql.org/docs/16/release-16.html#RELEASE-16-PRIVILEGES
		featureCreateRoleSelfGrant: semver.MustParseRange(">=16.0.0"),
		featureSecurityLabel:       semver.MustParseRange(">=11.0.0"),
	}
)

type DBConnection struct {
	*sql.DB

	client *Client

	// version is the version number of the database as determined by parsing the
	// output of `SELECT VERSION()`.x
	version semver.Version
}

// featureSupported returns true if a given feature is supported or not. This is
// slightly different from Config's featureSupported in that here we're
// evaluating against the fingerprinted version, not the expected version.
func (db *DBConnection) featureSupported(name featureName) bool {
	fn, found := featureSupported[name]
	if !found {
		// panic'ing because this is a provider-only bug
		panic(fmt.Sprintf("unknown feature flag %v", name))
	}

	return fn(db.version)
}

// isSuperuser returns true if connected user is a Postgres SUPERUSER
func (db *DBConnection) isSuperuser() (bool, error) {
	var superuser bool

	if err := db.QueryRow("SELECT rolsuper FROM pg_roles WHERE rolname = CURRENT_USER").Scan(&superuser); err != nil {
		return false, fmt.Errorf("could not check if current user is superuser: %w", err)
	}

	return superuser, nil
}

type ClientCertificateConfig struct {
	CertificatePath string
	KeyPath         string
	SSLInline       bool
}

// Config - provider config
type Config struct {
	Scheme                          string
	Host                            string
	Port                            int
	Username                        string
	Password                        string
	DatabaseUsername                string
	Superuser                       bool
	SSLMode                         string
	ApplicationName                 string
	Timeout                         int
	ConnectTimeoutSec               int
	MaxConns                        int
	MaxIdleConns                    int
	MaxConcurrentDatabases          int
	ConnMaxIdleTimeSec              int
	MaxRetries                      int
	ExpectedVersion                 semver.Version
	SSLClientCert                   *ClientCertificateConfig
	SSLRootCertPath                 string
	GCPIAMImpersonateServiceAccount string

	// scheduler and poolRegistry are reference types so Config value copies
	// made by NewClient share the same underlying state across all
	// per-database pools belonging to this provider configuration.
	scheduler    *dbScheduler
	poolRegistry *sync.Map // map[string]*registryEntry
}

// Client struct holding connection string
type Client struct {
	// Configuration for the client
	config Config

	databaseName string
}

// configInitMu is package-level: Config is value-copied by NewClient, so a
// per-Config mutex would not be shared across copies.
var configInitMu sync.Mutex

// ensureInit unconditionally takes the mutex — Go's memory model makes
// double-checked locking on c.poolRegistry / c.scheduler a race.
func (c *Config) ensureInit() {
	configInitMu.Lock()
	defer configInitMu.Unlock()
	if c.poolRegistry == nil {
		c.poolRegistry = &sync.Map{}
	}
	if c.MaxConcurrentDatabases > 0 && c.scheduler == nil {
		c.scheduler = newDBScheduler(c.MaxConcurrentDatabases)
	}
}

// NewClient returns client config for the specified database.
func (c *Config) NewClient(database string) *Client {
	c.ensureInit()
	return &Client{
		config:       *c,
		databaseName: database,
	}
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

func (c *Config) connParams() []string {
	params := map[string]string{}

	// sslmode and connect_timeout are not allowed with gocloud
	// (TLS is provided by gocloud directly)
	if c.Scheme == "postgres" {
		params["sslmode"] = c.SSLMode
		params["connect_timeout"] = strconv.Itoa(c.ConnectTimeoutSec)
	}

	if c.featureSupported(featureFallbackApplicationName) {
		params["fallback_application_name"] = c.ApplicationName
	}
	if c.SSLClientCert != nil {
		params["sslcert"] = c.SSLClientCert.CertificatePath
		params["sslkey"] = c.SSLClientCert.KeyPath
		if c.SSLClientCert.SSLInline {
			params["sslinline"] = strconv.FormatBool(c.SSLClientCert.SSLInline)
		}
	}

	if c.SSLRootCertPath != "" {
		params["sslrootcert"] = c.SSLRootCertPath
	}

	paramsArray := []string{}
	for key, value := range params {
		paramsArray = append(paramsArray, fmt.Sprintf("%s=%s", key, url.QueryEscape(value)))
	}

	return paramsArray
}

func (c *Config) connStr(database string) string {
	host := c.Host
	// For GCP, support both project/region/instance and project:region:instance
	// (The second one allows to use the output of google_sql_database_instance as host
	if c.Scheme == "gcppostgres" {
		host = strings.ReplaceAll(host, ":", "/")
	}

	connStr := fmt.Sprintf(
		"%s://%s:%s@%s:%d/%s?%s",
		c.Scheme,
		url.PathEscape(c.Username),
		url.PathEscape(c.Password),
		host,
		c.Port,
		database,
		strings.Join(c.connParams(), "&"),
	)

	return connStr
}

func (c *Config) getDatabaseUsername() string {
	if c.DatabaseUsername != "" {
		return c.DatabaseUsername
	}
	return c.Username
}

// Connect returns a cached *DBConnection for the receiver's (host, user,
// database) tuple. Failed attempts are not memoized — the registry entry is
// removed so the next caller retries from scratch.
func (c *Client) Connect() (*DBConnection, error) {
	c.config.ensureInit()
	dsn := c.config.connStr(c.databaseName)
	registry := c.config.poolRegistry
	entryAny, _ := registry.LoadOrStore(dsn, &registryEntry{})
	entry := entryAny.(*registryEntry)
	entry.once.Do(func() {
		entry.conn, entry.err = c.openAndPing(dsn)
		if entry.err != nil {
			registry.CompareAndDelete(dsn, entry)
		}
	})
	return entry.conn, entry.err
}

func (c *Client) openAndPing(dsn string) (*DBConnection, error) {
	var db *sql.DB
	var err error
	if c.config.Scheme == "postgres" {
		db = sql.OpenDB(&proxyConnector{dsn: dsn})
	} else if c.config.Scheme == "gcppostgres" && c.config.GCPIAMImpersonateServiceAccount != "" {
		db, err = openImpersonatedGCPDBConnection(context.Background(), dsn, c.config.GCPIAMImpersonateServiceAccount)
	} else {
		db, err = postgres.Open(context.Background(), dsn)
	}

	if err == nil {
		err = db.Ping()
	}
	if err != nil {
		// Release any physical conn that Ping may have already acquired —
		// Connect() retries failed entries.
		if db != nil {
			_ = db.Close()
		}
		errString := strings.Replace(err.Error(), c.config.Password, "XXXX", 2)
		return nil, fmt.Errorf("error connecting to PostgreSQL server %s (scheme: %s): %s", c.config.Host, c.config.Scheme, errString)
	}

	// Default MaxIdleConns=0 closes connections after use so DROP DATABASE
	// isn't blocked on PostgreSQL < 13. Users may opt in via
	// max_idle_connections.
	db.SetMaxIdleConns(c.config.MaxIdleConns)
	db.SetMaxOpenConns(c.config.MaxConns)
	if c.config.ConnMaxIdleTimeSec > 0 {
		db.SetConnMaxIdleTime(time.Duration(c.config.ConnMaxIdleTimeSec) * time.Second)
	}

	defaultVersion, _ := semver.Parse(defaultExpectedPostgreSQLVersion)
	version := &c.config.ExpectedVersion
	if defaultVersion.Equals(c.config.ExpectedVersion) {
		// Version hint not set by user, need to fingerprint
		version, err = fingerprintCapabilities(db)
		if err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("error detecting capabilities: %w", err)
		}
	}

	return &DBConnection{
		db,
		c,
		*version,
	}, nil
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

func openImpersonatedGCPDBConnection(ctx context.Context, dsn string, targetServiceAccountEmail string) (*sql.DB, error) {
	ts, err := impersonate.CredentialsTokenSource(ctx, impersonate.CredentialsConfig{
		TargetPrincipal: targetServiceAccountEmail,
		Scopes:          []string{"https://www.googleapis.com/auth/sqlservice.admin"},
	})
	if err != nil {
		return nil, fmt.Errorf("error creating token source with service account impersonation of %s: %w", targetServiceAccountEmail, err)
	}
	client, err := gcp.NewHTTPClient(gcp.DefaultTransport(), ts)
	if err != nil {
		return nil, fmt.Errorf("error creating HTTP client with service account impersonation of %s: %w", targetServiceAccountEmail, err)
	}
	certSource := cloudsql.NewCertSourceWithIAM(client, ts)
	opener := gcppostgres.URLOpener{CertSource: certSource}
	dbURL, err := url.Parse(dsn)
	if err != nil {
		return nil, fmt.Errorf("error parsing connection string: %w", err)
	}
	return opener.OpenPostgresURL(ctx, dbURL)
}
