# Migrate `gcppostgres` to the v2 Cloud SQL connector

**Date:** 2026-06-28
**Status:** Approved (design)

## Goal

Let the provider connect to a Cloud SQL for PostgreSQL instance that has **only a
private IP**, using **IAM authentication**, and optionally via **GCP DNS** (PSC
auto-DNS or a custom domain). Expose the various Cloud SQL connectivity options
through a clean, scenario-agnostic provider interface so public / private / PSC /
DNS / IAM / impersonation combinations are all reachable.

## Background

Today the `gcppostgres` scheme is served by `gocloud.dev/postgres/gcppostgres`,
which wraps the **cloudsql-proxy v1** cert library
(`github.com/GoogleCloudPlatform/cloudsql-proxy/proxy/certs`). That library:

- Connects by *instance connection name* (`project/region/instance`), resolving
  an IP from the Cloud SQL Admin API.
- Selects the IP via `IPAddrTypeOpts` (default `["PUBLIC","PRIVATE"]`). The
  recently merged `gcp_ip_type` option (PR #2) exposes `public`/`private` here.
- Has **no concept of DNS names or PSC** (verified by source inspection of
  `cloudsql-proxy@v1.33.9`).
- `EnableIAMLogin: true` only makes the *client certificate* IAM-compatible. The
  actual PostgreSQL password always comes from the DSN — gocloud copies the URL
  userinfo straight into the tunneled `postgres` connection. The v1 path never
  injects the OAuth token as the DB password.

The official successor is the **v2 connector**
`cloud.google.com/go/cloudsqlconn` (latest `v1.22.1`), which supports IAM
authN, public/private/PSC IP selection, DNS/domain resolution, and ships a
`database/sql` driver via `cloud.google.com/go/cloudsqlconn/postgres/pgxv5`.

### Verified v2 connector API

- Dialer options: `WithIAMAuthN()`, `WithIAMAuthNTokenSources(apiTS, loginTS)`,
  `WithTokenSource(ts)`, `WithDNSResolver()`, `WithDefaultDialOptions(...)`.
- Dial options (IP type): `WithPublicIP()`, `WithPrivateIP()`, `WithPSC()`,
  `WithAutoIP()` — `WithAutoIP` = "public if available, else private", i.e.
  identical to today's default `[PUBLIC,PRIVATE]` ordering.
- `WithDNSResolver()` resolves a domain via a **TXT** record whose value is the
  instance connection name (`project:region:instance`).
- `WithIAMAuthNTokenSources` scopes: API token source needs
  `sqlservice.admin` + `cloud-platform`; the login token source needs
  `sqlservice.login`.
- `pgxv5.RegisterDriver(name, opts...) (func() error, error)` registers a
  `database/sql` driver. Its DSN is **pgx keyword/value** with the instance
  connection name (or domain) in the `host` field, e.g.
  `host=my-project:us-central1:my-db user=myuser password=mypass dbname=...`.
  The driver extracts `host` as the connection name and dials through the
  connector.

## Decision

Replace the engine for `scheme=gcppostgres` with the v2 connector while keeping
the existing config UX behavior-identical, and add new options for the
previously unreachable scenarios. The AWS (`awspostgres`) and plain `postgres`
paths are untouched.

## Architecture

`Client.Connect()` dispatch becomes:

```
scheme == "postgres"     → postgresql-proxy driver (lib/pq + SOCKS)   [unchanged]
scheme == "gcppostgres"  → openGCPConnection()  via cloudsqlconn v2   [NEW]
else (awspostgres, …)    → gocloud postgres.Open()                    [unchanged]
```

New file `postgresql/gcp_connector.go`:

- `gcpDialerOptions(config *Config) ([]cloudsqlconn.Option, error)` — pure
  mapping from `Config` to connector options (testable in isolation).
- `gcpDSN(config *Config, database string) string` — builds the pgx
  keyword/value DSN (host = normalized connection name or domain). Separate from
  the existing URL `connStr` used by the other schemes.
- `gcpDriverName(config *Config) string` — deterministic name derived from a
  hash of the option-affecting fields.
- `openGCPConnection(ctx, config *Config, database string) (*sql.DB, error)` —
  registers the driver once per unique option-set (package-level
  `map[string]bool` + mutex, since `sql.Register`/`RegisterDriver` panic on
  duplicate names), then `sql.Open(name, dsn)`.

Deleted: `openImpersonatedGCPDBConnection` and all cloudsql-proxy-v1 / gocloud
`gcppostgres` / `gocloud.dev/gcp/cloudsql` usage.

## Config surface

| Provider arg | Type | Default | Behavior |
|---|---|---|---|
| `gcp_ip_type` | string | `""` | `""`→`WithAutoIP()`; `private`→`WithPrivateIP()`; `public`→`WithPublicIP()`; `psc`→`WithPSC()`. Extends the existing `public`/`private` enum by adding `psc`. |
| `gcp_iam_auth` | bool | `false` | `true`→`WithIAMAuthN()`: the IAM token is used as the DB password, `username` is the IAM principal, and no `password` is required. `false`→password taken from the DSN (current behavior). |
| `gcp_dns` | bool | `false` | `true`→`WithDNSResolver()`: `host` is treated as a domain name backed by a TXT record. Also auto-enabled when `host` is not shaped like `project:region:instance` / `project/region/instance`. |
| `gcp_iam_impersonate_service_account` | string | `""` | Impersonated token source. With `gcp_iam_auth=true`→`WithIAMAuthNTokenSources(apiTS, loginTS)`; otherwise→`WithTokenSource(apiTS)` (Admin API access only). |

Host normalization: accept `project/region/instance` (current docs form),
`project:region:instance` (the `google_sql_database_instance` output form), or a
DNS domain. Connection-name forms are normalized to colon form for the
connector; domains are passed through.

## Backward compatibility

Every existing configuration stays behavior-identical:

- Default opts + password auth → `WithAutoIP()` + DSN password ≡ today's
  `[PUBLIC,PRIVATE]` + DSN password.
- `gcp_ip_type=private` (PR #2) → `WithPrivateIP()`.
- `gcp_iam_impersonate_service_account` → impersonated Admin-API creds; password
  still from the DSN.
- IAM users who currently put an access token in the `password` field keep
  working with `gcp_iam_auth=false`, and may switch to `gcp_iam_auth=true` for
  automatic token injection.

`gcp_iam_auth` defaults to `false` specifically because that preserves all
current behavior; IAM database authentication is opt-in.

## Target scenario (the requester's instance)

VPC private IP + IAM, eventually dropping public IP:

```hcl
provider "postgresql" {
  scheme      = "gcppostgres"
  host        = "my-project/europe-west3/my-instance"
  port        = 5432
  username    = "sa-name@my-project.iam"   # IAM principal
  gcp_ip_type = "private"
  gcp_iam_auth = true
  superuser   = false
}
```

Connecting by domain (PSC auto-DNS or custom TXT domain) additionally sets
`gcp_dns = true` and uses the domain as `host`. Private-IP + IAM needs no DNS.

## Dependencies

- **Add:** `cloud.google.com/go/cloudsqlconn`,
  `cloud.google.com/go/cloudsqlconn/postgres/pgxv5` (transitively `jackc/pgx/v5`).
- **Remove from the GCP path:** `gocloud.dev/postgres/gcppostgres`,
  `gocloud.dev/gcp/cloudsql`, `github.com/GoogleCloudPlatform/cloudsql-proxy`
  (gocloud remains for `awspostgres`).

## Testing

- **Unit:**
  - `TestProvider` covers the new schema args (validation of `gcp_ip_type`
    including `psc`; bool args).
  - Table test for `gcpDialerOptions` over the permutation matrix (ip type ×
    iam_auth × dns × impersonation) asserting the expected option set / errors.
  - Test for `gcpDSN` host normalization (slash, colon, domain) and DSN output.
- **Acceptance:** real private-only instance verification stays manual via
  `dev_overrides`, documented (same posture as PR #2); not gated in CI.
- **Docs:** rewrite the GCP section of
  `website/docs/index.html.markdown` for the new options and
  private / PSC / DNS examples; update the argument reference.

## Out of scope

- No changes to AWS / Azure / plain `postgres` connection paths.
- No automatic CI acceptance against a live Cloud SQL instance.
- First-generation Cloud SQL instances (unsupported by the connector).

## Verifying an instance from gcloud

```bash
gcloud sql instances describe INSTANCE \
  --format="yaml(connectionName, dnsName, ipAddresses, \
pscServiceAttachmentLink, settings.ipConfiguration, settings.databaseFlags)"
```

- `ipAddresses[].type`: `PRIMARY` (public), `PRIVATE` (VPC), `OUTGOING`.
- `dnsName`: instance auto-DNS (present for PSC / DNS-enabled instances).
- `pscServiceAttachmentLink`: present only when PSC is enabled.
- `settings.databaseFlags`: `cloudsql.iam_authentication = on` enables IAM DB auth.
- `connectionName`: the `project:region:instance` the connector dials.
