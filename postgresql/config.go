package postgresql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/blang/semver"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/retry"
	"github.com/lib/pq"
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
	featureMaintainPrivilege
)

var (
	dbRegistryLock sync.Mutex
	dbRegistry     map[string]*DBConnection = make(map[string]*DBConnection, 1)

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

		// MAINTAIN privilege on tables (PG 17+)
		featureMaintainPrivilege: semver.MustParseRange(">=17.0.0"),
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
	MaxConnRetries                  int
	ConnectionRetryTimeoutSeconds   int
	ConnMaxLifetimeSeconds          int
	MaxConns                        int
	ExpectedVersion                 semver.Version
	SSLClientCert                   *ClientCertificateConfig
	SSLRootCertPath                 string
	GCPIAMImpersonateServiceAccount string
}

// Client struct holding connection string
type Client struct {
	// Configuration for the client
	config Config

	databaseName string
}

// NewClient returns client config for the specified database.
func (c *Config) NewClient(database string) *Client {
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

// Connect returns a copy to an sql.Open()'ed database connection wrapped in a DBConnection struct.
// Callers must return their database resources. Use of QueryRow() or Exec() is encouraged.
// Query() must have their rows.Close()'ed.
func (c *Client) Connect() (*DBConnection, error) {
	ctx := context.Background()
	dsn := c.config.connStr(c.databaseName)

	dbRegistryLock.Lock()
	conn, found := dbRegistry[dsn]
	dbRegistryLock.Unlock()
	if found {
		return conn, nil
	}

	// Dialing and retrying happen outside the lock so a slow/flaky connection
	// to one DSN doesn't stall Connect() calls for other DSNs.
	db, err := c.connectWithRetry(ctx, proxyDriverName, dsn)
	if err != nil {
		return nil, err
	}

	// We don't want to retain connection
	// So when we connect on a specific database which might be managed by terraform,
	// we don't keep opened connection in case of the db has to be dropped in the plan.
	db.SetMaxIdleConns(0)
	db.SetMaxOpenConns(c.config.MaxConns)
	db.SetConnMaxLifetime(time.Duration(c.config.ConnMaxLifetimeSeconds) * time.Second)

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

	conn = &DBConnection{
		db,
		c,
		*version,
	}

	dbRegistryLock.Lock()
	defer dbRegistryLock.Unlock()
	if existing, found := dbRegistry[dsn]; found {
		// Another goroutine won the race and already published a connection
		// for this DSN; keep that one and discard ours.
		_ = db.Close()
		return existing, nil
	}
	dbRegistry[dsn] = conn
	return conn, nil
}

// connectWithRetry opens a connection to dsn, retrying transient failures up
// to c.config.MaxConnRetries times or until c.config.ConnectionRetryTimeoutSeconds
// elapses, whichever comes first. postgresDriverName is the database/sql
// driver used for the "postgres" scheme (overridable by tests).
func (c *Client) connectWithRetry(ctx context.Context, postgresDriverName, dsn string) (*sql.DB, error) {
	var db *sql.DB
	var err error
	retryCount := 0

	connectRetryTimeout := time.Duration(c.config.ConnectionRetryTimeoutSeconds) * time.Second
	retryError := retry.RetryContext(ctx, connectRetryTimeout, func() *retry.RetryError {
		if c.config.Scheme == "postgres" {
			db, err = sql.Open(postgresDriverName, dsn)
		} else if c.config.Scheme == "gcppostgres" && c.config.GCPIAMImpersonateServiceAccount != "" {
			db, err = openImpersonatedGCPDBConnection(ctx, dsn, c.config.GCPIAMImpersonateServiceAccount)
		} else {
			db, err = postgres.Open(ctx, dsn)
		}
		if err == nil {
			err = db.PingContext(ctx)
		}

		if err == nil {
			return nil
		}

		if db != nil {
			_ = db.Close()
			db = nil
		}

		if !isRetryableConnectionError(err) || retryCount >= c.config.MaxConnRetries {
			return retry.NonRetryableError(err)
		}
		retryCount++
		return retry.RetryableError(err)
	})
	if retryError != nil {
		errString := strings.Replace(retryError.Error(), c.config.Password, "XXXX", 2)
		return nil, fmt.Errorf("error connecting to PostgreSQL server %s (scheme: %s): %s", c.config.Host, c.config.Scheme, errString)
	}

	return db, nil
}

// isRetryableConnectionError reports whether a connection failure is likely
// transient (network blips, server not ready yet) as opposed to a
// configuration error (bad credentials, unknown database) that will not be
// fixed by retrying.
func isRetryableConnectionError(err error) bool {
	if pqErr, ok := errors.AsType[*pq.Error](err); ok {
		switch pqErr.Code.Class() {
		case "28", // Invalid Authorization Specification
			"3D": // Invalid Catalog Name
			return false
		}
	}
	return true
}

// fingerprintCapabilities queries PostgreSQL to populate a local catalog of
// capabilities.  This is only run once per Client.
func fingerprintCapabilities(db *sql.DB) (*semver.Version, error) {
	var pgVersion string
	err := db.QueryRow(`SELECT VERSION()`).Scan(&pgVersion)
	if err != nil {
		return nil, fmt.Errorf("error PostgreSQL version: %w", err)
	}

	return parsePostgreSQLVersion(pgVersion)
}

// versionRe extracts the numeric version right after "PostgreSQL ", dropping vendor
// suffixes (AWS RDS, TDE builds, ...) that aren't valid semver.
var versionRe = regexp.MustCompile(`^PostgreSQL\s+(\d+(?:\.\d+){0,2})`)

// parsePostgreSQLVersion extracts a semver.Version from a `SELECT VERSION()` result.
func parsePostgreSQLVersion(pgVersion string) (*semver.Version, error) {
	matches := versionRe.FindStringSubmatch(pgVersion)
	if matches == nil {
		return nil, fmt.Errorf("error determining the server version: %q", pgVersion)
	}

	version, err := semver.ParseTolerant(matches[1])
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
