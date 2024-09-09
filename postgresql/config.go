package postgresql

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"unicode"

	"github.com/blang/semver"
	_ "github.com/lib/pq" // PostgreSQL db
	"gocloud.dev/postgres"
	_ "gocloud.dev/postgres/awspostgres"
	_ "gocloud.dev/postgres/gcppostgres"
)

type dbTypes uint

const (
	dbTypeCockroachdb dbTypes = iota
	dbTypePostgresql
)

type featureName uint

const (
	featureCreateRoleWith featureName = iota
	featureDatabaseOwnerRole
	featureDBAllowConnections
	featureDBIsTemplate
	featureDBTablespace
	featureUseDBTemplate
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
	fetureAclExplode
	fetureAclItem
	fetureTerminateBackendFunc
	fetureRoleConnectionLimit
	fetureRoleSuperuser
	featureRoleroleInherit
	fetureRoleEncryptedPass
	featureAdvisoryXactLock
	featureTransactionIsolation
	featureSysPrivileges
)

var (
	dbRegistryLock sync.Mutex
	dbRegistry     map[string]*DBConnection = make(map[string]*DBConnection, 1)

	// Mapping of feature flags to versions
	featureSupportedPostgres = map[featureName]semver.Range{
		// CREATE ROLE WITH
		featureCreateRoleWith: semver.MustParseRange(">=8.1.0"),

		// CREATE DATABASE has ALLOW_CONNECTIONS support
		featureDBAllowConnections: semver.MustParseRange(">=9.5.0"),

		// CREATE DATABASE has IS_TEMPLATE support
		featureDBIsTemplate: semver.MustParseRange(">=9.5.0"),

		// CREATE DATABASE use template
		featureUseDBTemplate: semver.MustParseRange(">=7.1.0"),

		// CREATE DATABASE in tablespace
		featureDBTablespace: semver.MustParseRange(">=8.0.0"),

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

		fetureAclExplode: semver.MustParseRange(">=9.0.0"),

		fetureAclItem: semver.MustParseRange(">=12.0.0"),

		fetureTerminateBackendFunc: semver.MustParseRange(">=8.0.0"),

		fetureRoleConnectionLimit: semver.MustParseRange(">=8.1.0"),

		fetureRoleSuperuser: semver.MustParseRange(">=8.1.0"),

		featureRoleroleInherit: semver.MustParseRange(">=8.1.0"),

		fetureRoleEncryptedPass: semver.MustParseRange(">=8.1.0"),

		featureAdvisoryXactLock: semver.MustParseRange(">=9.1.0"),
		//Postgresql do not support transaction isolation in Role level
		featureTransactionIsolation: semver.MustParseRange("<1.0.0"),
		featureSysPrivileges:        semver.MustParseRange("<1.0.0"),
	}

	// Mapping of feature flags to versions
	featureSupportedCockroachdb = map[featureName]semver.Range{
		// CREATE ROLE WITH
		featureCreateRoleWith: semver.MustParseRange(">=1.0.0"),

		// CREATE DATABASE has ALLOW_CONNECTIONS support
		featureDBAllowConnections: semver.MustParseRange("<1.0.0"),

		// CREATE DATABASE has IS_TEMPLATE support
		featureDBIsTemplate: semver.MustParseRange("<1.0.0"),

		// CREATE DATABASE use template
		featureUseDBTemplate: semver.MustParseRange("<1.0.0"),

		// CREATE DATABASE in tablespace
		featureDBTablespace: semver.MustParseRange("<1.0.0"),

		// https://www.postgresql.org/docs/9.0/static/libpq-connect.html
		// not supported in Cockroachdb
		featureFallbackApplicationName: semver.MustParseRange("<1.0.0"),

		// CREATE SCHEMA IF NOT EXISTS
		featureSchemaCreateIfNotExist: semver.MustParseRange(">=1.0.0"),

		// row-level security
		featureRLS: semver.MustParseRange("<1.0.0"),

		// CREATE ROLE has REPLICATION support.
		// not supported in Cockroachdb
		featureReplication: semver.MustParseRange("<1.0.0"),

		// CREATE EXTENSION support.
		// not supported in Cockroachdb
		featureExtension: semver.MustParseRange("<1.0.0"),

		// We do not support postgresql_grant and postgresql_default_privileges
		// for Cockroachdb < 21.2.17
		featurePrivileges: semver.MustParseRange(">=21.2.17"),

		// Object PROCEDURE support
		// not supported in Cockroachdb
		featureProcedure: semver.MustParseRange("<1.0.0"),

		// Object ROUTINE support
		// not supported in Cockroachdb
		featureRoutine: semver.MustParseRange("<1.0.0"),

		// ALTER DEFAULT PRIVILEGES has ON SCHEMAS support
		// for Cockroachdb < 21.2.17
		featurePrivilegesOnSchemas: semver.MustParseRange(">=21.2.17"),

		// DROP DATABASE WITH FORCE
		// not supported in Cockroachdb
		featureForceDropDatabase: semver.MustParseRange("<1.0.0"),

		// for CockroachDB pg_catalog >= 20.2.19 and above
		featurePid: semver.MustParseRange(">=20.2.19"),

		// attribute publish_via_partition_root for partition is supported
		// not supported in Cockroachdb
		featurePublishViaRoot: semver.MustParseRange("<1.0.0"),

		// attribute pubtruncate for publications is supported
		// not supported in Cockroachdb
		featurePubTruncate: semver.MustParseRange("<1.0.0"),

		// attribute pubtruncate for publications is supported
		// not supported in Cockroachdb
		featurePubWithoutTruncate: semver.MustParseRange("<1.0.0"),

		// publication is Supported
		// not supported in Cockroachdb
		featurePublication: semver.MustParseRange("<1.0.0"),

		// We do not support CREATE FUNCTION for Cockroachdb < 22.2.17
		featureFunction: semver.MustParseRange(">=22.2.17"),

		// CREATE SERVER support
		// not supported in Cockroachdb
		featureServer: semver.MustParseRange("<1.0.0"),

		featureDatabaseOwnerRole: semver.MustParseRange(">=20.2.19"),

		//aclexplode function not supported in Cockroachdb
		// see https://www.cockroachlabs.com/docs/stable/functions-and-operators
		fetureAclExplode: semver.MustParseRange("<1.0.0"),

		//cockroachdb does not support aclitem
		fetureAclItem: semver.MustParseRange("<1.0.0"),

		//pg_terminate_backend function not supported in Cockroachdb
		fetureTerminateBackendFunc: semver.MustParseRange("<1.0.0"),

		//Cockroachdb does not support connection limit
		fetureRoleConnectionLimit: semver.MustParseRange("<1.0.0"),

		//Cockroachdb does not support superuser
		fetureRoleSuperuser: semver.MustParseRange("<1.0.0"),

		//Cockroachdb does not role inherit
		featureRoleroleInherit: semver.MustParseRange("<1.0.0"),

		//Cockroachdb does not encrypt password
		fetureRoleEncryptedPass: semver.MustParseRange("<1.0.0"),

		// cockroach does not support pg_advisory_xact_lock
		// https://github.com/cockroachdb/cockroach/issues/13546
		featureAdvisoryXactLock: semver.MustParseRange("<1.0.0"),

		featureTransactionIsolation: semver.MustParseRange(">=23.2.0"),
		featureSysPrivileges:        semver.MustParseRange(">=22.2.0"),
	}
)

type DBConnection struct {
	*sql.DB

	client *Client

	// version is the version number of the database as determined by parsing the
	// output of `SELECT VERSION()`.x
	version semver.Version
	dbType  dbTypes
}

// featureSupported returns true if a given feature is supported or not. This is
// slightly different from Config's featureSupported in that here we're
// evaluating against the fingerprinted version, not the expected version.
func (db *DBConnection) featureSupported(name featureName) bool {
	var fn semver.Range
	var found bool
	if db.dbType == dbTypeCockroachdb {
		fn, found = featureSupportedCockroachdb[name]
	} else {
		fn, found = featureSupportedPostgres[name]
	}
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
	Scheme            string
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
	fn, found := featureSupportedPostgres[name]
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
	dbRegistryLock.Lock()
	defer dbRegistryLock.Unlock()

	dsn := c.config.connStr(c.databaseName)
	conn, found := dbRegistry[dsn]
	if !found {

		var db *sql.DB
		var err error
		var dbType dbTypes
		if c.config.Scheme == "postgres" {
			db, err = sql.Open(proxyDriverName, dsn)
		} else {
			db, err = postgres.Open(context.Background(), dsn)
		}

		if err == nil {
			err = db.Ping()
		}
		if err != nil {
			errString := strings.Replace(err.Error(), c.config.Password, "XXXX", 2)
			return nil, fmt.Errorf("Error connecting to PostgreSQL server %s (scheme: %s): %s", c.config.Host, c.config.Scheme, errString)
		}

		// We don't want to retain connection
		// So when we connect on a specific database which might be managed by terraform,
		// we don't keep opened connection in case of the db has to be dopped in the plan.
		db.SetMaxIdleConns(0)
		db.SetMaxOpenConns(c.config.MaxConns)

		defaultVersion, _ := semver.Parse(defaultExpectedPostgreSQLVersion)
		version := &c.config.ExpectedVersion
		if defaultVersion.Equals(c.config.ExpectedVersion) {
			// Version hint not set by user, need to fingerprint
			version, dbType, err = fingerprintCapabilities(db)
			if err != nil {
				_ = db.Close()
				return nil, fmt.Errorf("error detecting capabilities: %w", err)
			}
		}

		conn = &DBConnection{
			db,
			c,
			*version,
			dbType,
		}
		dbRegistry[dsn] = conn
	}

	return conn, nil
}

// fingerprintCapabilities queries PostgreSQL to populate a local catalog of
// capabilities.  This is only run once per Client.
func fingerprintCapabilities(db *sql.DB) (*semver.Version, dbTypes, error) {
	var pgVersion string
	var dbType dbTypes
	var version semver.Version
	err := db.QueryRow(`SELECT VERSION()`).Scan(&pgVersion)
	if err != nil {
		return nil, dbType, fmt.Errorf("error PostgreSQL version: %w", err)
	}

	// PostgreSQL 9.2.21 on x86_64-apple-darwin16.5.0, compiled by Apple LLVM version 8.1.0 (clang-802.0.42), 64-bit
	// PostgreSQL 9.6.7, compiled by Visual C++ build 1800, 64-bit
	fields := strings.FieldsFunc(pgVersion, func(c rune) bool {
		return unicode.IsSpace(c) || c == ','
	})
	if len(fields) < 2 {
		return nil, dbType, fmt.Errorf("error determining the server version: %q", pgVersion)
	}

	//version, err = semver.ParseTolerant(fields[1])
	dbTypeStr := fields[0]
	if dbTypeStr == "CockroachDB" {
		dbType = dbTypeCockroachdb
		version, err = semver.ParseTolerant(fields[2])
		version = semver.MustParse(strings.TrimPrefix(version.String(), "v"))
	} else {
		dbType = dbTypePostgresql
		version, err = semver.ParseTolerant(fields[1])
	}

	if err != nil {
		return nil, dbType, fmt.Errorf("error parsing version: %w", err)
	}

	return &version, dbType, nil
}
