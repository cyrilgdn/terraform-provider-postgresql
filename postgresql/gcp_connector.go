package postgresql

import (
	"strings"
)

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
