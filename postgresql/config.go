package postgresql

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"unicode"

	"github.com/Azure/go-autorest/autorest"
	"github.com/Azure/go-autorest/autorest/adal"
	"github.com/blang/semver"
	"github.com/hashicorp/go-azure-helpers/authentication"
	"github.com/hashicorp/go-azure-helpers/sender"
	_ "github.com/lib/pq" //PostgreSQL db
	"gocloud.dev/postgres"
	_ "gocloud.dev/postgres/awspostgres"
	_ "gocloud.dev/postgres/gcppostgres"
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
	featureForceDropDatabase
	featurePid
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

		// DROP DATABASE WITH FORCE
		// for Postgresql >= 13
		featureForceDropDatabase: semver.MustParseRange(">=13.0.0"),

		// Column procpid was replaced by pid in pg_stat_activity
		// for Postgresql >= 9.2 and above
		featurePid: semver.MustParseRange(">=9.2.0"),
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
}

// Config - provider config
type Config struct {
	Scheme                      string
	Host                        string
	Port                        int
	Username                    string
	Password                    string
	DatabaseUsername            string
	Superuser                   bool
	SSLMode                     string
	ApplicationName             string
	Timeout                     int
	ConnectTimeoutSec           int
	MaxConns                    int
	ExpectedVersion             semver.Version
	SSLClientCert               *ClientCertificateConfig
	SSLRootCertPath             string
	AzureADAuthenticationConfig *authentication.Config
}

// Client struct holding connection string
type Client struct {
	// Configuration for the client
	config Config

	databaseName string

	aadOAuthToken func() (string, error)

	// PostgreSQL lock on pg_catalog.  Many of the operations that Terraform
	// performs are not permitted to be concurrent.  Unlike traditional
	// PostgreSQL tables that use MVCC, many of the PostgreSQL system
	// catalogs look like tables, but are not in-fact able to be
	// concurrently updated.
	catalogLock sync.RWMutex
}

// NewClient returns client config for the specified database.
func (c *Config) NewClient(database string) (*Client, error) {
	aadOAuthToken, err := c.buildAzureADAuthenticationClient()
	if err != nil {
		return nil, fmt.Errorf("error while setting up Azure AD connection: %w", err)
	}

	return &Client{
		config:        *c,
		databaseName:  database,
		aadOAuthToken: aadOAuthToken,
	}, nil
}

func (c *Config) buildAzureADAuthenticationClient() (func() (string, error), error) {
	config := c.AzureADAuthenticationConfig
	if config == nil {
		return nil, nil
	}

	ctx := context.TODO()
	env, err := authentication.AzureEnvironmentByNameFromEndpoint(ctx, config.MetadataHost, config.Environment)
	if err != nil {
		return nil, err
	}

	var endpoint string
	switch strings.ToLower(c.AzureADAuthenticationConfig.Environment) {
	case "public":
		endpoint = "https://ossrdbms-aad.database.windows.net"
	case "usgovernment":
		endpoint = "https://ossrdbms-aad.database.usgovcloudapi.net"
	case "german":
		endpoint = "https://ossrdbms-aad.database.cloudapi.de"
	case "china":
		endpoint = "https://ossrdbms-aad.database.chinacloudapi.cn"
	default:
		return nil, fmt.Errorf("unsupported Azure environment for AzureAD authentication: %s", config.Environment)
	}

	s := sender.BuildSender("AzureAD")

	oauth, err := config.BuildOAuthConfig(env.ActiveDirectoryEndpoint)
	if err != nil {
		return nil, err
	}

	authorizer, err := config.GetAuthorizationToken(s, oauth, endpoint)
	if err != nil {
		return nil, err
	}

	ensureFresh := func(tokenProvider interface{}) error {
		// the ordering is important here, prefer RefresherWithContext if available
		if refresher, ok := tokenProvider.(adal.RefresherWithContext); ok {
			err = refresher.EnsureFreshWithContext(ctx)
		} else if refresher, ok := tokenProvider.(adal.Refresher); ok {
			err = refresher.EnsureFresh()
		}
		if err != nil {
			return fmt.Errorf("failed to refresh token: %w", err)
		}
		return nil
	}

	switch v := authorizer.(type) {
	case *autorest.BearerAuthorizer:
		return func() (string, error) {
			tokenProvider := v.TokenProvider()
			err := ensureFresh(tokenProvider)
			if err != nil {
				return "", err
			}
			return tokenProvider.OAuthToken(), nil
		}, nil
	case *autorest.MultiTenantBearerAuthorizer:
		return func() (string, error) {
			tokenProvider := v.TokenProvider()
			err := ensureFresh(tokenProvider)
			if err != nil {
				return "", err
			}
			return tokenProvider.PrimaryOAuthToken(), nil
		}, nil
	default:
		return nil, fmt.Errorf("unsupported AzureAD authentication method: %T", v)
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
	params := map[string]string{
		"connect_timeout": strconv.Itoa(c.ConnectTimeoutSec),
	}

	// sslmode is not allowed with gocloud
	// (TLS is provided by gocloud directly)
	if c.Scheme == "postgres" {
		params["sslmode"] = c.SSLMode
	}

	if c.featureSupported(featureFallbackApplicationName) {
		params["fallback_application_name"] = c.ApplicationName
	}
	if c.SSLClientCert != nil {
		params["sslcert"] = c.SSLClientCert.CertificatePath
		params["sslkey"] = c.SSLClientCert.KeyPath
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

func (c *Config) getDatabaseUsername() string {
	if c.DatabaseUsername != "" {
		return c.DatabaseUsername
	}
	return c.Username
}

func (c *Client) connStr(database string) (string, error) {
	host := c.config.Host

	// For GCP, support both project/region/instance and project:region:instance
	// (The second one allows to use the output of google_sql_database_instance as host
	if c.config.Scheme == "gcppostgres" {
		host = strings.ReplaceAll(host, ":", "/")
	}

	password := c.config.Password
	if c.aadOAuthToken != nil {
		var err error
		password, err = c.aadOAuthToken()
		if err != nil {
			return "", fmt.Errorf("failed to get Azure AD access token: %w", err)
		}
	}

	connStr := fmt.Sprintf(
		"%s://%s:%s@%s:%d/%s?%s",
		c.config.Scheme,
		url.QueryEscape(c.config.Username),
		url.QueryEscape(password),
		host,
		c.config.Port,
		database,
		strings.Join(c.config.connParams(), "&"),
	)

	return connStr, nil
}

// Connect returns a copy to an sql.Open()'ed database connection wrapped in a DBConnection struct.
// Callers must return their database resources. Use of QueryRow() or Exec() is encouraged.
// Query() must have their rows.Close()'ed.
func (c *Client) Connect() (*DBConnection, error) {
	dbRegistryLock.Lock()
	defer dbRegistryLock.Unlock()

	dsn, err := c.connStr(c.databaseName)
	if err != nil {
		return nil, fmt.Errorf("failed to format DSN: %w", err)
	}
	conn, found := dbRegistry[dsn]
	if !found {

		var db *sql.DB
		var err error
		if c.config.Scheme == "postgres" {
			db, err = sql.Open("postgres", dsn)
		} else {
			db, err = postgres.Open(context.Background(), dsn)
		}
		if err != nil {
			return nil, fmt.Errorf("Error connecting to PostgreSQL server %s (scheme: %s): %w", c.config.Host, c.config.Scheme, err)
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
			version, err = fingerprintCapabilities(db)
			if err != nil {
				db.Close()
				return nil, fmt.Errorf("error detecting capabilities: %w", err)
			}
		}

		conn = &DBConnection{
			db,
			c,
			*version,
		}

		if c.aadOAuthToken != nil {
			// TODO(Sem Mulder): Allow the user to toggle this in the provider settings? Or in the resources that set roles?
			_, err = conn.Exec("SET aad_validate_oids_in_tenant = off;")
			if err != nil {
				return nil, fmt.Errorf("error disabling 'aad_validate_oids_in_tenant': %w", err)
			}
		}

		dbRegistry[dsn] = conn
	}

	return conn, nil
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
