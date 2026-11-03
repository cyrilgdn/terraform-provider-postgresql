# Changelog

## 1.51.0 (Unreleased)

### Bug Fixes

- **crdb_role_setting**: Send numeric `setting_value` unquoted so integer settings such as `transaction_rows_read_err` no longer fail with "requires an integer value" ([DEV-146590](https://riskified.atlassian.net/browse/DEV-146590))

## 1.50.0 (August 27, 2026)

### Bug Fixes

- **changefeed**: Fix `table_list` update/read to use the full list of tables so adding a table to an existing CDC emits `ALTER CHANGEFEED … ADD` ([DEV-138674](https://riskified.atlassian.net/browse/DEV-138674))

## 1.49.0 (June 1, 2026)

### Features

- **crdb_role_setting**: Add new `postgresql_crdb_role_setting` resource for managing CockroachDB default session variable overrides per role (`ALTER ROLE … SET / RESET`)

## 1.48.0 (June 1, 2026)

### Features

- **retry**: Add `QueryRetry` and `ExecRetry` helpers to wrap transient CockroachDB errors (connection reset, serialization failure, operator intervention) with configurable exponential back-off
- **provider**: Add `max_retries` and `retry_max_delay_ms` provider fields to control retry behaviour

## 1.47.0 (April 10, 2026)

### Bug Fixes

- **role**: Fix role deletion failing when role owns objects in other databases — `REASSIGN OWNED BY` and `DROP OWNED BY` are now executed in every non-system database, not just the provider's default database ([DEV-128384](https://riskified.atlassian.net/browse/DEV-128384))
- **grant_role**: Fix `postgresql_grant_role` read returning an error when the grant doesn't exist instead of removing the resource from state, causing plan failures on fresh databases ([DEV-128384](https://riskified.atlassian.net/browse/DEV-128384))

## 1.46.0  (April 10, 2026)

### Features

- **changefeed**: Add `key_column` and `unordered` support to changefeed resource ([DEV-125419](https://riskified.atlassian.net/browse/DEV-125419))
- **changefeed**: Add `compression` option to changefeed resource ([DEV-128858](https://riskified.atlassian.net/browse/DEV-128858))

### Improvements

- Upgrade Go from 1.20 to 1.25 and modernize CI
- Simplify golangci-lint config: use defaults, fix lint violations
- Add tfplugindocs templates with rich documentation from old website

## 1.45.0 (April 9, 2026)

### Breaking Changes

- **Remove PostgreSQL support** — the provider now targets CockroachDB only
  - Removed resources: `extension`, `publication`, `subscription`, `replication_slot`, `physical_replication_slot`, `server`, `user_mapping`
  - Removed provider fields: `aws_rds_iam_auth`, `aws_rds_iam_profile`, `aws_rds_iam_region`, `azure_identity_auth`, `azure_tenant_id`, `superuser`, `scheme`
  - Default port changed from `5432` to `26257`
  - Default `expected_version` changed from `9.0.0` to `22.2.0`
- **role**: Removed unsupported CockroachDB role attributes: `connection_limit`, `superuser`, `inherit`, `encrypted_password`, `replication`, `encrypted` ([DEV-123267](https://riskified.atlassian.net/browse/DEV-123267))
- **database**: Removed attributes: `template`, `tablespace_name`, `allow_connections`, `is_template`
- **grant**: Removed object types: `foreign_data_wrapper`, `foreign_server`, `column`
- **schema**: Removed `policy` attribute

### Features

- **database**: Add `deletion_protection` attribute to `postgresql_database`
- **schema**: Add `deletion_protection` attribute to `postgresql_schema`

### Bug Fixes

- Fix 4 provider bugs + add acceptance test suite for all CRDB versions
- Fix SQL injection in `readDatabaseRolePriviges`: use `pq.QuoteIdentifier`/`pq.QuoteLiteral`
- Fix SQL injection in `readSchemaRolePriviges`: use `pq.QuoteIdentifier`/`pq.QuoteLiteral`
- Fix SQL injection in function/routine grants query: use `pq.QuoteLiteral`
- Fix SQL injection in table grants query: use `pq.QuoteIdentifier`/`pq.QuoteLiteral`
- Fix SQL injection in `readGrantRole`: use `pq.QuoteIdentifier` for role names
- Fix CRDB function type drift and body whitespace drift
- Fix CRDB volatility parsing: scan header before dollar-quote delimiter
- Fall back to `pg_db_role_setting` when `rolconfig` is empty in CockroachDB 25.4+
- Gate `setRoleBypassRLS` in Update with `featureRLS` check
- Warn users that role renames destroy and recreate the role

### Improvements

- Refactor: replace transaction use with direct database connections for improved error handling and simplicity
- Remove `deferredRollback` & `startTransaction`
- Remove `pgLockRole` no-op and its call sites
- Remove PostgreSQL provider documentation and supporting website files
