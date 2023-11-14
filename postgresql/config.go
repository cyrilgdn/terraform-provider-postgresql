package postgresql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"unicode"

	"github.com/blang/semver"
	"github.com/lib/pq"
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
	passwordCacheLock sync.Mutex = sync.Mutex{}
	passwordCache                = make(map[string]string)

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
	Scheme                   string
	Host                     string
	Port                     int
	Username                 string
	Password                 string
	DatabaseUsername         string
	Superuser                bool
	SSLMode                  string
	ApplicationName          string
	Timeout                  int
	ConnectTimeoutSec        int
	MaxConns                 int
	ExpectedVersion          semver.Version
	SSLClientCert            *ClientCertificateConfig
	SSLRootCertPath          string
	JumpHost                 string
	TunneledPort             int
	PasswordCommand          string
	FallbackToStaticPassword bool

	ctx context.Context
}

// Client struct holding connection string
type Client struct {
	// Configuration for the client
	config Config

	databaseName string

	dbRegistryLock sync.Mutex
	dbRegistry     map[string]*DBConnection
}

// NewClient returns client config for the specified database.
func (c *Config) NewClient(database string) *Client {
	client := &Client{
		config:       *c,
		databaseName: database,
		dbRegistry:   map[string]*DBConnection{},
	}
	go client.connectionWatcher()
	return client
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

func (c *Config) connStr(database string) (string, error) {
	var host string
	var port int
	if c.shouldUseJumpHost() {
		host = "localhost"
		port = c.TunneledPort
	} else {
		host = c.Host
		port = c.Port
	}

	// For GCP, support both project/region/instance and project:region:instance
	// (The second one allows to use the output of google_sql_database_instance as host
	if c.Scheme == "gcppostgres" {
		host = strings.ReplaceAll(host, ":", "/")
	}

	password := c.Password
	if c.PasswordCommand != "" && !c.FallbackToStaticPassword {
		log.Printf("user %s password %s command %s", c.Username, c.Password, c.PasswordCommand)
		log.Printf("[DEBUG] Trying to get the password from an external command")
		newPassword, err := c.getCachedPassword()
		if err != nil {
			return "", fmt.Errorf("%w", err)
		}
		password = newPassword
		log.Printf("[DEBUG] Password fetched successfuly")
	} else {
		log.Printf("[DEBUG] Using specified password for authentication")
	}

	connStr := fmt.Sprintf(
		"%s://%s:%s@%s:%d/%s?%s",
		c.Scheme,
		url.QueryEscape(c.Username),
		url.QueryEscape(password),
		host,
		port,
		database,
		strings.Join(c.connParams(), "&"),
	)

	return connStr, nil
}

func (c *Config) getCachedPassword() (string, error) {
	passwordCacheLock.Lock()
	defer passwordCacheLock.Unlock()

	cacheKey := fmt.Sprintf("%s%d%s", c.Host, c.Port, c.Username)
	if password, ok := passwordCache[cacheKey]; ok {
		return password, nil
	}
	newPassword, err := getCommandOutput(c.ctx, "bash", "-ec", c.PasswordCommand)
	if err != nil {
		return "", fmt.Errorf("failed to execute the password command %w", err)
	}
	passwordCache[cacheKey] = newPassword
	return newPassword, nil
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
	// Lock only the public method as the internal one calls it recursivelly when falling back
	// to static password
	c.dbRegistryLock.Lock()
	defer c.dbRegistryLock.Unlock()
	if c.config.shouldUseJumpHost() {
		err := connectToJumpHost(&c.config)
		if err != nil {
			return nil, fmt.Errorf("failed to open a tunnel to jumphost %w", err)
		}
	}

	return c.tryConnectWithFallback()
}

func (c *Client) connect() (*DBConnection, error) {
	dsn, err := c.config.connStr(c.databaseName)
	if err != nil {
		return nil, fmt.Errorf("failed to get connection string %w", err)
	}
	conn, found := c.dbRegistry[dsn]
	if found {
		log.Printf("Reusing database connection")
		rows, err := conn.Query("SELECT 1")
		defer func() {
			_ = rows.Close()
		}()
		if err != nil {
			delete(c.dbRegistry, dsn)
			return nil, fmt.Errorf("failed to ping database %w", err)
		}
		return conn, nil
	}
	log.Printf("Creating a new database connection")
	var db *sql.DB
	if c.config.Scheme == "postgres" {
		db, err = sql.Open("postgres", dsn)
	} else {
		db, err = postgres.Open(c.config.ctx, dsn)
	}
	if err != nil {
		return nil, fmt.Errorf("Error connecting to PostgreSQL server %s (scheme: %s): %w", c.config.Host, c.config.Scheme, err)
	}

	// We don't want to retain connection
	// So when we connect on a specific database which might be managed by terraform,
	// we don't keep opened connection in case of the db has to be dopped in the plan.
	// TODO: For RDS usage this breaks the connection after a grant to rds_iam is used
	// db.SetMaxIdleConns(0)
	var maxConnections int
	if c.config.MaxConns == 1 {
		// This provider acquires a lock on pg_advisory_xact_lock using a separate connection
		// so it is required to have at least 2 connections
		maxConnections = 2
	}
	db.SetMaxOpenConns(maxConnections)

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
	c.dbRegistry[dsn] = conn

	return conn, nil
}

func (c *Client) tryConnectWithFallback() (*DBConnection, error) {
	var err error
	conn, err := c.connect()
	if err == nil {
		return conn, nil
	}

	var driverError *pq.Error

	if !errors.As(err, &driverError) || driverError.Code.Class() != pgInvalidAuth {
		// Just wrap the error for the stacktrace
		return nil, fmt.Errorf("%w", err)
	}

	useFallback := !c.config.FallbackToStaticPassword && c.config.Password != "" && c.config.PasswordCommand != ""
	if useFallback {
		c.config.FallbackToStaticPassword = true
		defer func() {
			c.config.FallbackToStaticPassword = false
		}()
		log.Printf("[DEBUG] Falling back to static password")
		conn, err := c.connect()
		if err != nil {
			panic(err)
		}
		return conn, err
	}

	return nil, fmt.Errorf("%w", err)
}

func (c *Client) connectionWatcher() {
	<-c.config.ctx.Done()
	c.closeConnections()
}

func (c *Client) closeConnections() {
	c.dbRegistryLock.Lock()
	defer c.dbRegistryLock.Unlock()
	for _, connection := range c.dbRegistry {
		err := connection.Close()
		if err != nil {
			log.Printf("[ERROR] Failed to close database connection %v", err)
		}
	}
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

func (c *Config) shouldUseJumpHost() bool {
	return c.JumpHost != "" && c.TunneledPort != 0
}
