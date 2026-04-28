---
layout: "postgresql"
page_title: "PostgreSQL: postgresql_role_database_setting"
sidebar_current: "docs-postgresql-resource-postgresql_role_database_setting"
description: |-
  Manages a per-database role configuration parameter via ALTER ROLE ... IN DATABASE ... SET ...
---

# postgresql\_role\_database\_setting

The ``postgresql_role_database_setting`` resource manages a single per-database role configuration parameter, i.e. a row in the system catalog [`pg_db_role_setting`](https://www.postgresql.org/docs/current/catalog-pg-db-role-setting.html). It corresponds to the SQL statement:

```sql
ALTER ROLE <role> IN DATABASE <database> SET <parameter> = '<value>';
```

Unlike the global `assume_role` attribute on `postgresql_role` (which translates to `ALTER ROLE … SET role = …` and applies cluster-wide), this resource scopes the setting to a specific database. Common use cases:

- Auto-switching a developer role into a project-owned group role on connect: `parameter = "role"`, `value = "<project>_db_owner"`.
- Setting a project-specific `search_path` for a shared role.
- Tuning per-database `statement_timeout` for a service account.

## Example: per-database `assume role` for an Entra ID developer

```hcl
resource "postgresql_role" "owner" {
  name  = "app_db_owner"
  login = false
}

resource "postgresql_role" "dev" {
  name    = "alice@example.com"
  login   = true
  inherit = true
  roles   = [postgresql_role.owner.name]
}

resource "postgresql_database" "db" {
  name  = "app_db"
  owner = postgresql_role.owner.name
}

resource "postgresql_role_database_setting" "dev_assume_owner" {
  role      = postgresql_role.dev.name
  database  = postgresql_database.db.name
  parameter = "role"
  value     = postgresql_role.owner.name
}
```

When `alice@example.com` connects to `app_db`, their `current_role` is automatically set to `app_db_owner`, so any objects created by ad-hoc DDL or migrations are owned by the group role instead of the individual user.

## Example: setting multiple parameters on the same (role, database) pair

You can declare independent resources for each parameter; they coexist in the same `pg_db_role_setting` row without conflicting:

```hcl
resource "postgresql_role_database_setting" "search_path" {
  role      = postgresql_role.dev.name
  database  = postgresql_database.db.name
  parameter = "search_path"
  value     = "app, public"
}

resource "postgresql_role_database_setting" "statement_timeout" {
  role      = postgresql_role.dev.name
  database  = postgresql_database.db.name
  parameter = "statement_timeout"
  value     = "5min"
}
```

Concurrent operations against the same `(role, database)` pair are serialized internally with a transactional advisory lock, so parallel `terraform apply` of multiple resources on the same pair is safe.

## Caveat: per-DB `role` requires intact membership

Per-database `role = <target>` only takes effect on connect if the connecting role is actually a member of `<target>`. The membership and the per-DB setting are stored in **different catalogs** (`pg_auth_members` vs `pg_db_role_setting`) and managed by different resources, so they can drift apart silently.

The most common way they drift: `postgresql_role` manages memberships **authoritatively**. Its Update path REVOKEs every membership of the role found in `pg_auth_members`, then GRANTs only those listed in the resource's `roles = [...]`. Update fires when any attribute changes, or whenever Read sees memberships not in the config — the default state right after `terraform import`, unless the engineer manually copies all memberships into config.

If the same role is also targeted by `postgresql_grant_role` rows from other modules (or by manual `GRANT` from a DBA), those memberships get silently revoked on the next `postgresql_role` apply. PostgreSQL then warns `permission denied to set role` on the role's next connect, and the session falls back to `current_role = <role>` — `pg_db_role_setting` is untouched but ineffective.

The recommended patterns:

- For **shared identities** (e.g. roles created externally and grant-wired into multiple projects): manage memberships exclusively via `postgresql_grant_role` (non-authoritative, scoped to one tuple), and do not declare the role as `postgresql_role`. Each project module then owns only its own grant + per-DB setting.
- If `postgresql_role` must manage the role (e.g. for password / login attributes), add `lifecycle { ignore_changes = [roles, assume_role, search_path, statement_timeout] }` so it stops fighting the foreign grants.

See [issue #285](https://github.com/cyrilgdn/terraform-provider-postgresql/issues/285) for the underlying `postgresql_role` behaviour.

## Argument Reference

* `role` - (Required) The role whose per-database setting is managed. Forces a new resource on change.
* `database` - (Required) The database in which the setting applies. Forces a new resource on change.
* `parameter` - (Required) The configuration parameter (any GUC name accepted by `ALTER ROLE`, e.g. `role`, `search_path`, `statement_timeout`). Forces a new resource on change.
* `value` - (Required) The value to assign to the parameter for this `(role, database)` pair. The provider quotes the value as a string literal; PostgreSQL will interpret and canonicalize it according to the parameter's type.

## Import

Existing settings can be imported using the composite ID `<role>:<database>:<parameter>`:

```shell
terraform import postgresql_role_database_setting.dev_assume_owner \
  'alice@example.com:app_db:role'
```

Names with `@`, `.`, or mixed case are supported as-is (the provider quotes identifiers correctly when emitting `ALTER ROLE`). PostgreSQL also allows literal `:` and `\` in quoted identifiers; in the import ID they must be backslash-escaped (`\:` and `\\`) so the three components remain unambiguous. The same encoding is what the resource emits for the state ID, so you can copy a state ID directly:

```shell
# role = "alice:dev", database = "app:blue", parameter = "role"
terraform import postgresql_role_database_setting.dev_assume_owner \
  'alice\:dev:app\:blue:role'
```

Use single quotes (or double-escape) when invoking `terraform import` from a shell, otherwise the shell will eat the backslashes.
