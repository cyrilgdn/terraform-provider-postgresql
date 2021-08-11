package postgresql

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"log"
	"net"
	"net/url"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/blang/semver"
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
	JumpHost          string
	TunneledPort      int
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

	connStr := fmt.Sprintf(
		"%s://%s:%s@%s:%d/%s?%s",
		c.Scheme,
		url.QueryEscape(c.Username),
		url.QueryEscape(c.Password),
		host,
		port,
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

	if c.config.shouldUseJumpHost() {
		err := c.connectToJumpHost()
		if err != nil {
			return nil, fmt.Errorf("Failed to open a tunnel to jumphost %w", err)
		}
	}
	dsn := c.config.connStr(c.databaseName)
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
		dbRegistry[dsn] = conn
	}

	return conn, nil
}

func (c *Client) connectToJumpHost() error {
	log.Println("[DEBUG] Connecting to jumphost")
	args := []string{
		c.config.JumpHost,
		"-o", "UserKnownHostsFile=/dev/null", "-o", "StrictHostKeyChecking=no",
		"-L", fmt.Sprintf("127.0.0.1:%d:%s:%d", c.config.TunneledPort, c.config.Host, c.config.Port),
		"-N",
	}
	var combinedOutput bytes.Buffer

	log.Printf("[DEBUG] Calling ssh with %v\n", args)
	cmd := exec.Command("ssh")
	cmd.Args = append(cmd.Args, args...)
	cmd.Stderr = &combinedOutput
	cmd.Stdout = &combinedOutput
	err := cmd.Start()
	if err != nil {
		return fmt.Errorf("failed to start ssh tunnel: %w", err)
	}
	errorChannel := make(chan error)
	go func() {
		errorChannel <- cmd.Wait()
	}()
	for try := 0; try < 10; try++ {
		select {
		case err := <-errorChannel:
			return fmt.Errorf("ssh exited: %w %s", err, combinedOutput.String())
		case <-time.After(time.Millisecond * 100):
			_, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", c.config.TunneledPort))
			if err == nil {
				log.Printf("[DEBUG] Failed to connect on tunnel port %d\n", c.config.TunneledPort)
				return nil
			}
			time.Sleep(1 * time.Second)
		}
	}

	return fmt.Errorf("ssh failed to connect: %s", combinedOutput.String())
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
