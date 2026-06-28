# GCP Cloud SQL v2 Connector Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the cloudsql-proxy v1 engine behind `scheme=gcppostgres` with the official v2 connector (`cloud.google.com/go/cloudsqlconn`) so the provider can connect to private-IP-only Cloud SQL instances using IAM auth, plus PSC and DNS, while keeping all existing configs behavior-identical.

**Architecture:** All `gcppostgres` connections route through a new `openGCPConnection` that registers a `pgxv5` `database/sql` driver wired to a `cloudsqlconn.Dialer`. Provider config maps to dialer options via a small pure `gcpConnSpec`. The `postgres` and `awspostgres`/gocloud paths are untouched.

**Tech Stack:** Go 1.24, terraform-plugin-sdk/v2, `cloud.google.com/go/cloudsqlconn` v1.22.1, `cloud.google.com/go/cloudsqlconn/postgres/pgxv5`, `jackc/pgx/v5`.

**Prerequisite:** Branch `gcp-cloudsql-v2-connector` is rebased on `main` including PR #2 (the `gcp_ip_type` option). The spec lives at `docs/superpowers/specs/2026-06-28-gcp-cloudsql-v2-connector-design.md`.

**Environment note:** `go` is installed via Homebrew. If `go` is not on PATH in a fresh shell, prefix commands with `export PATH="$(brew --prefix)/bin:$PATH";`.

---

## File Structure

- `postgresql/gcp_connector.go` — **new**. Owns the entire GCP v2 path: `gcpSpec`, `gcpConnSpec`, host helpers, DSN builder, driver-name + registration, dialer-option mapping, `openGCPConnection`.
- `postgresql/gcp_connector_test.go` — **new**. Unit tests for the pure helpers (`isGCPConnectionName`, `gcpHost`, `gcpKVQuote`, `gcpDSN`, `gcpConnSpec`, `gcpDriverName`, non-impersonation `gcpDialerOptions`).
- `postgresql/config.go` — **modify**. Add `GCPIAMAuth`/`GCPDNS` fields; reroute the `gcppostgres` branch in `Connect()`; delete `openGCPDBConnection`; drop now-unused imports.
- `postgresql/provider.go` — **modify**. Add `gcp_iam_auth` + `gcp_dns` schema args; add `psc` to `gcp_ip_type`; wire the two new fields in `providerConfigure`.
- `postgresql/provider_test.go` — **modify**. Add a schema-presence test for the GCP options.
- `go.mod` / `go.sum` — **modify** via `go get` + `go mod tidy`.
- `website/docs/index.html.markdown` — **modify**. Rewrite the GCP section + argument reference.

---

## Task 1: Add the v2 connector dependencies

**Files:**
- Modify: `go.mod`, `go.sum`

- [ ] **Step 1: Add the modules**

Run:
```bash
export PATH="$(brew --prefix)/bin:$PATH"
go get cloud.google.com/go/cloudsqlconn@v1.22.1
go get cloud.google.com/go/cloudsqlconn/postgres/pgxv5@v1.22.1
```
Expected: `go.mod` gains `cloud.google.com/go/cloudsqlconn v1.22.1` and pulls in `github.com/jackc/pgx/v5`.

- [ ] **Step 2: Verify it resolves**

Run: `go build ./... 2>&1 | tail -5`
Expected: builds with no errors (nothing uses the new deps yet, but the module graph must resolve).

- [ ] **Step 3: Commit**

```bash
git add go.mod go.sum
git commit -m "build: add cloud.google.com/go/cloudsqlconn v2 connector deps"
```

---

## Task 2: Add Config fields and provider schema args

**Files:**
- Modify: `postgresql/config.go` (struct `Config`, ~line 173-190)
- Modify: `postgresql/provider.go` (schema ~line 123-132; `providerConfigure` ~line 385-401)
- Test: `postgresql/provider_test.go`

- [ ] **Step 1: Write the failing test**

Add to `postgresql/provider_test.go`:
```go
func TestProviderGCPOptions(t *testing.T) {
	p := Provider()
	for _, key := range []string{
		"gcp_ip_type",
		"gcp_iam_auth",
		"gcp_dns",
		"gcp_iam_impersonate_service_account",
	} {
		if _, ok := p.Schema[key]; !ok {
			t.Errorf("provider schema missing %q", key)
		}
	}

	// gcp_ip_type must accept "psc"
	v := p.Schema["gcp_ip_type"].ValidateFunc
	if v == nil {
		t.Fatal("gcp_ip_type has no ValidateFunc")
	}
	if _, errs := v("psc", "gcp_ip_type"); len(errs) != 0 {
		t.Errorf("gcp_ip_type rejected \"psc\": %v", errs)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./postgresql/ -run TestProviderGCPOptions -v`
Expected: FAIL — `provider schema missing "gcp_iam_auth"` and `gcp_ip_type rejected "psc"`.

- [ ] **Step 3: Add the Config fields**

In `postgresql/config.go`, in the `Config` struct, after `GCPIPType string`:
```go
	GCPIPType                       string
	GCPIAMAuth                      bool
	GCPDNS                          bool
```

- [ ] **Step 4: Extend the `gcp_ip_type` schema and add two new args**

In `postgresql/provider.go`, replace the `gcp_ip_type` block with the version below and add the two new blocks after it:
```go
			"gcp_ip_type": {
				Type:        schema.TypeString,
				Optional:    true,
				Default:     "",
				Description: "(`gcppostgres` only) IP address type of the Cloud SQL instance: `public`, `private`, or `psc`. If unset, the public IP is preferred with private as a fallback.",
				ValidateFunc: validation.StringInSlice([]string{
					"public",
					"private",
					"psc",
				}, false),
			},

			"gcp_iam_auth": {
				Type:        schema.TypeBool,
				Optional:    true,
				Default:     false,
				Description: "(`gcppostgres` only) If true, authenticate to the database using GCP IAM (the IAM token is used as the password; set `username` to the IAM principal and leave `password` empty).",
			},

			"gcp_dns": {
				Type:        schema.TypeBool,
				Optional:    true,
				Default:     false,
				Description: "(`gcppostgres` only) If true, treat `host` as a DNS domain name backed by a TXT record resolving to the instance connection name. Auto-enabled when `host` is not in `project:region:instance` form.",
			},
```

- [ ] **Step 5: Wire the new fields in `providerConfigure`**

In `postgresql/provider.go`, in the `config := Config{...}` literal, after `GCPIPType: d.Get("gcp_ip_type").(string),`:
```go
			GCPIPType:                       d.Get("gcp_ip_type").(string),
			GCPIAMAuth:                      d.Get("gcp_iam_auth").(bool),
			GCPDNS:                          d.Get("gcp_dns").(bool),
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test ./postgresql/ -run 'TestProvider' -v`
Expected: PASS — `TestProvider`, `TestProvider_impl`, `TestProviderGCPOptions`.

- [ ] **Step 7: Commit**

```bash
git add postgresql/config.go postgresql/provider.go postgresql/provider_test.go
git commit -m "feat: add gcp_iam_auth and gcp_dns options, allow gcp_ip_type=psc"
```

---

## Task 3: Host helpers (`isGCPConnectionName`, `gcpHost`)

**Files:**
- Create: `postgresql/gcp_connector.go`
- Create: `postgresql/gcp_connector_test.go`

- [ ] **Step 1: Write the failing test**

Create `postgresql/gcp_connector_test.go`:
```go
package postgresql

import "testing"

func TestIsGCPConnectionName(t *testing.T) {
	cases := map[string]bool{
		"proj:region:inst": true,
		"proj/region/inst": true,
		"db.example.com":   false,
		"proj:region":      false,
		"":                 false,
		"a::c":             false,
	}
	for host, want := range cases {
		if got := isGCPConnectionName(host); got != want {
			t.Errorf("isGCPConnectionName(%q) = %v, want %v", host, got, want)
		}
	}
}

func TestGCPHost(t *testing.T) {
	cases := map[string]string{
		"proj/region/inst": "proj:region:inst",
		"proj:region:inst": "proj:region:inst",
		"db.example.com":   "db.example.com",
	}
	for host, want := range cases {
		if got := gcpHost(host); got != want {
			t.Errorf("gcpHost(%q) = %q, want %q", host, got, want)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./postgresql/ -run 'TestIsGCPConnectionName|TestGCPHost' -v`
Expected: FAIL — `undefined: isGCPConnectionName`, `undefined: gcpHost`.

- [ ] **Step 3: Create the file with the helpers**

Create `postgresql/gcp_connector.go`:
```go
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
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./postgresql/ -run 'TestIsGCPConnectionName|TestGCPHost' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add postgresql/gcp_connector.go postgresql/gcp_connector_test.go
git commit -m "feat: add GCP connection-name host helpers"
```

---

## Task 4: DSN builder (`gcpKVQuote`, `gcpDSN`)

**Files:**
- Modify: `postgresql/gcp_connector.go`
- Modify: `postgresql/gcp_connector_test.go`

- [ ] **Step 1: Write the failing test**

Add to `postgresql/gcp_connector_test.go`:
```go
func TestGCPKVQuote(t *testing.T) {
	cases := map[string]string{
		"simple":   "'simple'",
		"a b":      "'a b'",
		"o'brien":  `'o\'brien'`,
		`back\sl`:  `'back\\sl'`,
	}
	for in, want := range cases {
		if got := gcpKVQuote(in); got != want {
			t.Errorf("gcpKVQuote(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestGCPDSN(t *testing.T) {
	// password auth includes the password
	pw := &Config{Host: "proj/region/inst", Port: 5432, Username: "u", Password: "p", ApplicationName: "Terraform provider"}
	got := gcpDSN(pw, "mydb")
	for _, want := range []string{"host='proj:region:inst'", "port=5432", "user='u'", "dbname='mydb'", "password='p'", "application_name='Terraform provider'"} {
		if !strings.Contains(got, want) {
			t.Errorf("gcpDSN password auth = %q, missing %q", got, want)
		}
	}

	// IAM auth omits the password even when set
	iam := &Config{Host: "proj:region:inst", Port: 5432, Username: "sa@proj.iam", Password: "ignored", GCPIAMAuth: true}
	got = gcpDSN(iam, "mydb")
	if strings.Contains(got, "password=") {
		t.Errorf("gcpDSN IAM auth should omit password, got %q", got)
	}
	if !strings.Contains(got, "user='sa@proj.iam'") {
		t.Errorf("gcpDSN IAM auth = %q, missing IAM user", got)
	}
}
```

Add `"strings"` to the test file's imports.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./postgresql/ -run 'TestGCPKVQuote|TestGCPDSN' -v`
Expected: FAIL — `undefined: gcpKVQuote`, `undefined: gcpDSN`.

- [ ] **Step 3: Implement the builders**

Add to `postgresql/gcp_connector.go` (and add `"fmt"` to its imports):
```go
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
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./postgresql/ -run 'TestGCPKVQuote|TestGCPDSN' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add postgresql/gcp_connector.go postgresql/gcp_connector_test.go
git commit -m "feat: add GCP pgx DSN builder"
```

---

## Task 5: Config → spec mapping (`gcpConnSpec`)

**Files:**
- Modify: `postgresql/gcp_connector.go`
- Modify: `postgresql/gcp_connector_test.go`

- [ ] **Step 1: Write the failing test**

Add to `postgresql/gcp_connector_test.go`:
```go
func TestGCPConnSpec(t *testing.T) {
	cases := []struct {
		name    string
		in      *Config
		want    gcpSpec
		wantErr bool
	}{
		{"default", &Config{Host: "p:r:i"}, gcpSpec{IPType: "auto", UseDNS: false, IAMAuth: false}, false},
		{"private", &Config{Host: "p:r:i", GCPIPType: "private"}, gcpSpec{IPType: "private"}, false},
		{"psc", &Config{Host: "p:r:i", GCPIPType: "psc"}, gcpSpec{IPType: "psc"}, false},
		{"iam", &Config{Host: "p:r:i", GCPIAMAuth: true}, gcpSpec{IPType: "auto", IAMAuth: true}, false},
		{"dns flag", &Config{Host: "p:r:i", GCPDNS: true}, gcpSpec{IPType: "auto", UseDNS: true}, false},
		{"domain host", &Config{Host: "db.example.com"}, gcpSpec{IPType: "auto", UseDNS: true}, false},
		{"impersonate", &Config{Host: "p:r:i", GCPIAMImpersonateServiceAccount: "sa@p.iam"}, gcpSpec{IPType: "auto", Impersonate: "sa@p.iam"}, false},
		{"invalid", &Config{Host: "p:r:i", GCPIPType: "bogus"}, gcpSpec{}, true},
	}
	for _, c := range cases {
		got, err := gcpConnSpec(c.in)
		if c.wantErr {
			if err == nil {
				t.Errorf("%s: expected error, got nil", c.name)
			}
			continue
		}
		if err != nil {
			t.Errorf("%s: unexpected error %v", c.name, err)
			continue
		}
		if got != c.want {
			t.Errorf("%s: gcpConnSpec = %+v, want %+v", c.name, got, c.want)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./postgresql/ -run TestGCPConnSpec -v`
Expected: FAIL — `undefined: gcpSpec`, `undefined: gcpConnSpec`.

- [ ] **Step 3: Implement the spec type and mapping**

Add to `postgresql/gcp_connector.go`:
```go
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
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./postgresql/ -run TestGCPConnSpec -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add postgresql/gcp_connector.go postgresql/gcp_connector_test.go
git commit -m "feat: add gcpConnSpec config mapping"
```

---

## Task 6: Driver name (`gcpDriverName`)

**Files:**
- Modify: `postgresql/gcp_connector.go`
- Modify: `postgresql/gcp_connector_test.go`

- [ ] **Step 1: Write the failing test**

Add to `postgresql/gcp_connector_test.go`:
```go
func TestGCPDriverName(t *testing.T) {
	a := gcpDriverName(gcpSpec{IPType: "private", IAMAuth: true})
	b := gcpDriverName(gcpSpec{IPType: "private", IAMAuth: true})
	c := gcpDriverName(gcpSpec{IPType: "public", IAMAuth: true})

	if a != b {
		t.Errorf("same spec gave different names: %q vs %q", a, b)
	}
	if a == c {
		t.Errorf("different specs gave same name: %q", a)
	}
	if !strings.HasPrefix(a, "cloudsql-postgres-") {
		t.Errorf("unexpected driver name %q", a)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./postgresql/ -run TestGCPDriverName -v`
Expected: FAIL — `undefined: gcpDriverName`.

- [ ] **Step 3: Implement**

Add to `postgresql/gcp_connector.go` (add `"crypto/sha256"` and `"strconv"` to imports):
```go
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
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./postgresql/ -run TestGCPDriverName -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add postgresql/gcp_connector.go postgresql/gcp_connector_test.go
git commit -m "feat: add deterministic GCP driver name"
```

---

## Task 7: Dialer options (`gcpDialerOptions`)

**Files:**
- Modify: `postgresql/gcp_connector.go`
- Modify: `postgresql/gcp_connector_test.go`

- [ ] **Step 1: Write the failing test**

Add to `postgresql/gcp_connector_test.go` (add `"context"` to imports):
```go
func TestGCPDialerOptionsNoImpersonation(t *testing.T) {
	// Non-impersonation specs build options without touching the network.
	specs := []gcpSpec{
		{IPType: "auto"},
		{IPType: "private", IAMAuth: true},
		{IPType: "psc", UseDNS: true},
		{IPType: "public"},
	}
	for _, s := range specs {
		opts, err := gcpDialerOptions(context.Background(), s)
		if err != nil {
			t.Errorf("gcpDialerOptions(%+v) error: %v", s, err)
		}
		if len(opts) == 0 {
			t.Errorf("gcpDialerOptions(%+v) returned no options", s)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./postgresql/ -run TestGCPDialerOptionsNoImpersonation -v`
Expected: FAIL — `undefined: gcpDialerOptions`.

- [ ] **Step 3: Implement**

Add to `postgresql/gcp_connector.go` (add `"context"`, `"cloud.google.com/go/cloudsqlconn"`, `"google.golang.org/api/impersonate"` to imports):
```go
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
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./postgresql/ -run TestGCPDialerOptionsNoImpersonation -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add postgresql/gcp_connector.go postgresql/gcp_connector_test.go
git commit -m "feat: map GCP spec to cloudsqlconn dialer options"
```

---

## Task 8: `openGCPConnection` + wire into `Connect()`, delete old path

**Files:**
- Modify: `postgresql/gcp_connector.go`
- Modify: `postgresql/config.go` (`Connect()` ~line 300-304; delete `openGCPDBConnection` ~line 368-404; imports ~line 3-22)

- [ ] **Step 1: Add `openGCPConnection`**

Add to `postgresql/gcp_connector.go` (this adds the remaining imports `"database/sql"`, `"sync"`, and `"cloud.google.com/go/cloudsqlconn/postgres/pgxv5"` — the full, final import block for the file is shown after this snippet):
```go
var (
	gcpDriverMu        sync.Mutex
	gcpRegisteredNames = map[string]bool{}
)

// openGCPConnection opens a Cloud SQL connection through the v2 connector,
// registering a pgxv5 database/sql driver once per distinct dialer option-set.
func openGCPConnection(ctx context.Context, config *Config, database string) (*sql.DB, error) {
	spec, err := gcpConnSpec(config)
	if err != nil {
		return nil, err
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
	gcpDriverMu.Unlock()

	return sql.Open(name, gcpDSN(config, database))
}
```

The final import block of `postgresql/gcp_connector.go` is:
```go
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
```

- [ ] **Step 2: Reroute the `gcppostgres` branch in `Connect()`**

In `postgresql/config.go`, replace the dispatch block (currently lines ~298-304):
```go
		if c.config.Scheme == "postgres" {
			db, err = sql.Open(proxyDriverName, dsn)
		} else if c.config.Scheme == "gcppostgres" && (c.config.GCPIAMImpersonateServiceAccount != "" || c.config.GCPIPType != "") {
			db, err = openGCPDBConnection(context.Background(), dsn, &c.config)
		} else {
			db, err = postgres.Open(context.Background(), dsn)
		}
```
with:
```go
		if c.config.Scheme == "postgres" {
			db, err = sql.Open(proxyDriverName, dsn)
		} else if c.config.Scheme == "gcppostgres" {
			db, err = openGCPConnection(context.Background(), &c.config, c.databaseName)
		} else {
			db, err = postgres.Open(context.Background(), dsn)
		}
```

- [ ] **Step 3: Delete the old `openGCPDBConnection`**

In `postgresql/config.go`, delete the entire `openGCPDBConnection` function (the block starting at the comment `// openGCPDBConnection opens a Cloud SQL connection through a customized` through its closing `}`).

- [ ] **Step 4: Drop now-unused imports in `config.go`**

In `postgresql/config.go`, remove these import lines (they were only used by the deleted function):
```go
	"github.com/GoogleCloudPlatform/cloudsql-proxy/proxy/certs"
	"gocloud.dev/gcp"
	"gocloud.dev/postgres/gcppostgres"
	"golang.org/x/oauth2"
	"google.golang.org/api/impersonate"
```
Keep `"context"`, `"net/url"`, `"strings"`, `"gocloud.dev/postgres"`, and `_ "gocloud.dev/postgres/awspostgres"` — they are still used.

- [ ] **Step 5: Build to verify wiring and imports**

Run: `go build ./... 2>&1 | tail -20`
Expected: builds clean. If the compiler reports an unused or missing import in `config.go`, adjust that single import line accordingly (e.g. `context` and `net/url` and `strings` must remain).

- [ ] **Step 6: Run the full unit suite**

Run: `go test ./postgresql/ -run 'TestProvider|TestGCP|TestIsGCP|TestConfig' -v 2>&1 | tail -30`
Expected: PASS for all listed tests.

- [ ] **Step 7: Commit**

```bash
git add postgresql/gcp_connector.go postgresql/config.go
git commit -m "feat: route gcppostgres through v2 connector, remove cloudsql-proxy v1 path"
```

---

## Task 9: Tidy modules and full verification

**Files:**
- Modify: `go.mod`, `go.sum`

- [ ] **Step 1: Tidy**

Run:
```bash
export PATH="$(brew --prefix)/bin:$PATH"
go mod tidy
```
Expected: `github.com/GoogleCloudPlatform/cloudsql-proxy` and `gocloud.dev/postgres/gcppostgres`-only transitive deps are dropped or demoted to indirect as appropriate; `cloud.google.com/go/cloudsqlconn` and `jackc/pgx/v5` are present.

- [ ] **Step 2: Confirm the v1 proxy lib is gone from the direct requires**

Run: `grep -n 'cloudsql-proxy' go.mod || echo "removed"`
Expected: either `removed`, or it appears only as `// indirect` (acceptable if still pulled by gocloud). It must NOT be a direct dependency.

- [ ] **Step 3: Build + vet + full test**

Run:
```bash
go build ./...
go vet ./postgresql/
go test ./postgresql/ -run 'TestProvider|TestGCP|TestIsGCP|TestConfig' -v 2>&1 | tail -30
```
Expected: build exit 0; vet clean; all unit tests PASS.

- [ ] **Step 4: Commit**

```bash
git add go.mod go.sum
git commit -m "build: go mod tidy after v2 connector migration"
```

---

## Task 10: Documentation

**Files:**
- Modify: `website/docs/index.html.markdown` (argument reference ~line 186-189; GCP section)

- [ ] **Step 1: Update the argument reference**

In `website/docs/index.html.markdown`, in the arguments list, replace the existing `gcp_ip_type` bullet and add the two new ones:
```markdown
* `gcp_ip_type` - (Optional) (`gcppostgres` only) IP address type of the Cloud SQL instance: `public`, `private`, or `psc`. If unset, the public IP is preferred and the private IP is used as a fallback. Set to `private` for instances without a public IP, see [GCP](#gcp).
* `gcp_iam_auth` - (Optional) (`gcppostgres` only) If `true`, authenticate to the database using GCP IAM. The IAM access token is used as the password; set `username` to the IAM principal and leave `password` empty. The instance must have the `cloudsql.iam_authentication` flag enabled, see [GCP](#gcp).
* `gcp_dns` - (Optional) (`gcppostgres` only) If `true`, `host` is treated as a DNS domain name backed by a TXT record that resolves to the instance connection name. Auto-enabled when `host` is not in `project:region:instance` form, see [GCP](#gcp).
```

- [ ] **Step 2: Update the GCP section examples**

In `website/docs/index.html.markdown`, in the GCP section, replace the private-IP paragraph/example added by PR #2 with the following block (private-IP + IAM, and a DNS variant):
````markdown
For instances that have no public IP, set `gcp_ip_type` to `private` (or `psc` for Private Service Connect). The connection then targets the instance's private IP, so the machine running Terraform must have network access to it (inside the VPC, or via VPC peering / Cloud VPN / a private worker pool). Combine with `gcp_iam_auth` to authenticate as an IAM principal:

```hcl
provider "postgresql" {
  scheme       = "gcppostgres"
  host         = "test-project/europe-west3/test-instance"
  port         = 5432

  username     = "sa-name@test-project.iam"
  gcp_ip_type  = "private"
  gcp_iam_auth = true

  superuser = false
}
```

To connect by DNS domain name (a TXT record resolving to the instance connection name, or a PSC auto-DNS name), set `gcp_dns = true` and use the domain as `host`:

```hcl
provider "postgresql" {
  scheme       = "gcppostgres"
  host         = "db.prod.example.com"
  port         = 5432

  username     = "sa-name@test-project.iam"
  gcp_ip_type  = "private"
  gcp_iam_auth = true
  gcp_dns      = true

  superuser = false
}
```

Connections are made through the official [Cloud SQL Go connector](https://github.com/GoogleCloudPlatform/cloud-sql-go-connector); the `host` accepts both `project/region/instance` and `project:region:instance` connection-name forms.

See also:

- https://cloud.google.com/docs/authentication/production
- https://cloud.google.com/sql/docs/postgres/iam-logins
- https://cloud.google.com/sql/docs/postgres/private-ip
- https://cloud.google.com/sql/docs/postgres/connect-auth-proxy#dns
````

- [ ] **Step 3: Commit**

```bash
git add website/docs/index.html.markdown
git commit -m "docs: document gcp_iam_auth, gcp_dns, and gcp_ip_type=psc"
```

---

## Self-Review notes (for the implementer)

- **Manual acceptance (not CI):** before pinning a release, verify against a real private-only instance using `dev_overrides` pointing at the local build — connect with `gcp_ip_type=private` + `gcp_iam_auth=true` and confirm a grant resource applies. This mirrors PR #2's posture and is out of scope for the unit suite.
- **pgx DSN params:** the GCP path uses pgx parameter names (`application_name`, `connect_timeout`), not lib/pq's `fallback_application_name`. This is intentional — the connector path does not go through `connStr`/`connParams`.
- **`connStr` is still the dbRegistry cache key** for gcppostgres; it remains unique per host/user/db. `openGCPConnection` builds its own DSN separately.
