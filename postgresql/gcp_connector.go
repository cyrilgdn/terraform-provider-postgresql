package postgresql

import (
	"fmt"
	"strings"
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
