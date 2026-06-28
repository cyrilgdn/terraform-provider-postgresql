package postgresql

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"sync"

	"cloud.google.com/go/cloudsqlconn"
	"cloud.google.com/go/cloudsqlconn/postgres/pgxv5"
	"google.golang.org/api/impersonate"
)

// gcpKVQuote quotes a value for a pgx keyword/value DSN.
func gcpKVQuote(v string) string {
	v = strings.ReplaceAll(v, `\`, `\\`)
	v = strings.ReplaceAll(v, `'`, `\'`)
	return "'" + v + "'"
}

// gcpDSN builds a pgx keyword/value DSN. The pgxv5 connector driver reads the
// host field as the Cloud SQL instance connection name (or DNS domain) and
// dials it through the connector. When IAM auth is enabled the password is
// omitted so the connector injects the IAM token instead.
func gcpDSN(config *Config, database string) string {
	parts := []string{
		"host=" + gcpKVQuote(gcpHost(config.Host)),
		fmt.Sprintf("port=%d", config.Port),
		"user=" + gcpKVQuote(config.Username),
		"dbname=" + gcpKVQuote(database),
	}
	if !config.GCPIAMAuth && config.Password != "" {
		parts = append(parts, "password="+gcpKVQuote(config.Password))
	}
	if config.ApplicationName != "" {
		parts = append(parts, "application_name="+gcpKVQuote(config.ApplicationName))
	}
	if config.ConnectTimeoutSec > 0 {
		parts = append(parts, fmt.Sprintf("connect_timeout=%d", config.ConnectTimeoutSec))
	}
	return strings.Join(parts, " ")
}

// isGCPConnectionName reports whether host looks like a Cloud SQL instance
// connection name ("project:region:instance" or "project/region/instance")
// rather than a DNS domain name.
func isGCPConnectionName(host string) bool {
	parts := strings.Split(strings.ReplaceAll(host, "/", ":"), ":")
	if len(parts) != 3 {
		return false
	}
	for _, p := range parts {
		if p == "" {
			return false
		}
	}
	return true
}

// gcpHost normalizes a connection name to colon form for the connector and
// passes DNS domain names through unchanged.
func gcpHost(host string) string {
	if isGCPConnectionName(host) {
		return strings.ReplaceAll(host, "/", ":")
	}
	return host
}

// gcpSpec is the connector-relevant subset of Config, derived once and used to
// build both the driver name and the dialer options.
type gcpSpec struct {
	IPType      string // "auto" | "public" | "private" | "psc"
	UseDNS      bool
	IAMAuth     bool
	Impersonate string
}

// gcpDriverName is a deterministic database/sql driver name for a given spec.
// Drivers are registered once per distinct option-set; the per-connection host,
// user, db and password are supplied through the DSN at sql.Open time.
func gcpDriverName(spec gcpSpec) string {
	key := strings.Join([]string{
		spec.IPType,
		strconv.FormatBool(spec.UseDNS),
		strconv.FormatBool(spec.IAMAuth),
		spec.Impersonate,
	}, "|")
	sum := sha256.Sum256([]byte(key))
	return fmt.Sprintf("cloudsql-postgres-%x", sum[:8])
}

// gcpDialerOptions maps a spec to cloudsqlconn dialer options. Impersonation
// branches build token sources, which require GCP credentials at call time.
func gcpDialerOptions(ctx context.Context, spec gcpSpec) ([]cloudsqlconn.Option, error) {
	var dialOpt cloudsqlconn.DialOption
	switch spec.IPType {
	case "private":
		dialOpt = cloudsqlconn.WithPrivateIP()
	case "public":
		dialOpt = cloudsqlconn.WithPublicIP()
	case "psc":
		dialOpt = cloudsqlconn.WithPSC()
	default: // "auto"
		dialOpt = cloudsqlconn.WithAutoIP()
	}
	opts := []cloudsqlconn.Option{cloudsqlconn.WithDefaultDialOptions(dialOpt)}

	if spec.UseDNS {
		opts = append(opts, cloudsqlconn.WithDNSResolver())
	}

	switch {
	case spec.Impersonate != "" && spec.IAMAuth:
		apiTS, err := impersonate.CredentialsTokenSource(ctx, impersonate.CredentialsConfig{
			TargetPrincipal: spec.Impersonate,
			Scopes: []string{
				"https://www.googleapis.com/auth/sqlservice.admin",
				"https://www.googleapis.com/auth/cloud-platform",
			},
		})
		if err != nil {
			return nil, fmt.Errorf("error creating API token source impersonating %s: %w", spec.Impersonate, err)
		}
		loginTS, err := impersonate.CredentialsTokenSource(ctx, impersonate.CredentialsConfig{
			TargetPrincipal: spec.Impersonate,
			Scopes:          []string{"https://www.googleapis.com/auth/sqlservice.login"},
		})
		if err != nil {
			return nil, fmt.Errorf("error creating login token source impersonating %s: %w", spec.Impersonate, err)
		}
		opts = append(opts, cloudsqlconn.WithIAMAuthNTokenSources(apiTS, loginTS))
	case spec.Impersonate != "":
		ts, err := impersonate.CredentialsTokenSource(ctx, impersonate.CredentialsConfig{
			TargetPrincipal: spec.Impersonate,
			Scopes:          []string{"https://www.googleapis.com/auth/sqlservice.admin"},
		})
		if err != nil {
			return nil, fmt.Errorf("error creating token source impersonating %s: %w", spec.Impersonate, err)
		}
		opts = append(opts, cloudsqlconn.WithTokenSource(ts))
	case spec.IAMAuth:
		opts = append(opts, cloudsqlconn.WithIAMAuthN())
	}

	return opts, nil
}

func gcpConnSpec(config *Config) (gcpSpec, error) {
	ipType := config.GCPIPType
	if ipType == "" {
		ipType = "auto"
	}
	switch ipType {
	case "auto", "public", "private", "psc":
	default:
		return gcpSpec{}, fmt.Errorf("invalid gcp_ip_type %q (want public, private, or psc)", config.GCPIPType)
	}
	return gcpSpec{
		IPType:      ipType,
		UseDNS:      config.GCPDNS || !isGCPConnectionName(config.Host),
		IAMAuth:     config.GCPIAMAuth,
		Impersonate: config.GCPIAMImpersonateServiceAccount,
	}, nil
}

var (
	gcpDriverMu        sync.Mutex
	gcpRegisteredNames = map[string]bool{}
)

// openGCPConnection opens a Cloud SQL connection through the v2 connector,
// registering a pgxv5 database/sql driver once per distinct dialer option-set.
func openGCPConnection(ctx context.Context, config *Config, database string) (*sql.DB, error) {
	spec, err := gcpConnSpec(config)
	if err != nil {
		return nil, fmt.Errorf("error building GCP connection spec: %w", err)
	}
	name := gcpDriverName(spec)

	gcpDriverMu.Lock()
	if !gcpRegisteredNames[name] {
		opts, err := gcpDialerOptions(ctx, spec)
		if err != nil {
			gcpDriverMu.Unlock()
			return nil, err
		}
		if _, err := pgxv5.RegisterDriver(name, opts...); err != nil {
			gcpDriverMu.Unlock()
			return nil, fmt.Errorf("error registering Cloud SQL connector driver: %w", err)
		}
		gcpRegisteredNames[name] = true
	}
	// Unlock before sql.Open so the connection setup does not hold the mutex.
	gcpDriverMu.Unlock()

	return sql.Open(name, gcpDSN(config, database))
}
