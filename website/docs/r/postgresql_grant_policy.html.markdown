---
layout: "postgresql"
page_title: "PostgreSQL: postgresql_grant_policy"
sidebar_current: "docs-postgresql-resource-postgresql_grant_policy"
description: |-
  Creates and manages, in a single aggregated resource, every grant held by a role in a database.
---

# postgresql\_grant\_policy

The ``postgresql_grant_policy`` resource manages, as a single unit, every grant held by a `role` in
a `database`.

It is the coarse-grained counterpart of [`postgresql_grant`](postgresql_grant.html): instead of one
Terraform resource per `(database, schema, role, object_type)` tuple, a single
`postgresql_grant_policy` resource owns every grant for a given `(database, role)` pair. This avoids
the state cardinality and per-resource advisory-lock contention that many independent
`postgresql_grant` resources for the same role can cause. Because every grant for the role is
reconciled inside a single transaction, this resource does not need `depends_on` chains between
grants to serialize them.

~> **Note:** This resource needs Postgresql version 9 or above. Declaring `object_type = "procedure"`
requires Postgresql 11 or above.

~> **Warning: `authoritative = true`.** When `authoritative` is `true`, this resource takes full
ownership of every grant held by `role` in `database`, for the object types it manages (database,
schema, table, sequence, function, procedure, foreign_data_wrapper, foreign_server). On **every
apply**, and on **destroy**, it **revokes** any such grant found in Postgres that is not declared in
`grants` — including ones created outside Terraform (manually, by another tool, or by a
`postgresql_grant` resource). Do not mix `authoritative = true` with `postgresql_grant`, or manual
`GRANT`, for the same `(database, role)` pair, or those grants will be revoked on the next apply.
`authoritative` defaults to `false` precisely so that adopting this resource against an existing
production database cannot silently revoke privileges you didn't intend to touch; opt into
`authoritative = true` only once you are certain `grants` fully describes the desired state.

## Usage

```hcl
resource "postgresql_grant_policy" "product_service_ro" {
  database = "product_service"
  role     = "product_service_role_ro"

  # If true, any grant held by the role in PG not defined in this block will be revoked.
  # If false, it only ensures defined grants are present (additive mode).
  authoritative = false

  grants = [
    {
      object_type = "database"
      privileges  = ["CONNECT"]
    },
    {
      schema      = "public"
      object_type = "schema"
      privileges  = ["USAGE"]
    },
    {
      schema      = "public"
      object_type = "table"
      privileges  = ["SELECT"]
      # objects           = ["orders", "order_items"] # Optional. Omitted means every table in the schema.
      # with_grant_option = false                      # Optional, defaults to false.
    }
  ]
}
```

## Argument Reference

* `database` - (Required, Forces new resource) The database to manage grants for this role.
* `role` - (Required, Forces new resource) The role to grant privileges to.
* `authoritative` - (Optional) Defaults to `false`. If `true`, every grant found for `role` in
  `database` that is not declared in `grants` (for the object types managed by this resource:
  database, schema, table, sequence, function, procedure, foreign_data_wrapper, foreign_server) is
  revoked on apply and on destroy — see the warning above. If `false` (the safe default), the
  resource only ensures the grants declared in `grants` are present (additive mode); it never
  revokes grants it did not declare, on apply or on destroy.
* `grants` - (Required) The list of grants to apply for this `(database, role)` policy. Each entry
  supports:
  * `schema` - (Optional) For `object_type = "schema"`, the schema being granted on. For
    `table`/`sequence`/`function`/`procedure`, the schema containing the objects — required for
    those four types. Must be empty for `database`/`foreign_data_wrapper`/`foreign_server`.
  * `object_type` - (Required) The PostgreSQL object type to grant privileges on (one of: database,
    schema, table, sequence, function, procedure, foreign_data_wrapper, foreign_server).
  * `objects` - (Optional) The specific objects to grant privileges on. Required (at least one) for
    `foreign_data_wrapper`/`foreign_server` — there is no "every foreign data wrapper/server" grant
    form in Postgres. Disallowed for `database`/`schema`. For `table`/`sequence`/`function`/
    `procedure`, omitted/empty means every object of that type in the schema (`ALL <TYPE>S IN
    SCHEMA`); a non-empty list scopes the grant to exactly those objects.
  * `privileges` - (Required) List of privileges to grant. An empty list revokes all privileges for
    this entry.
  * `with_grant_option` - (Optional) Defaults to `false`. Permits the grant recipient to grant it to
    others. Cannot be `true` when `role` is `public`.

Each `(schema, object_type)` combination must appear at most once across `grants`.

`object_type = "column"` is not supported by this resource: a column grant is keyed by `(table,
column, single privilege)`, which does not fit the `(schema, object_type)` grant-entry shape every
other object type here shares. Use [`postgresql_grant`](postgresql_grant.html) directly for
column-level grants.

## Attributes Reference

In addition to the arguments above, the following attribute is exported:

* `id` - The ID of the resource, computed as `<database>_<role>`.

## Import

`postgresql_grant_policy` can be imported using `<database>/<role>`, e.g.

```
terraform import postgresql_grant_policy.product_service_ro product_service/product_service_role_ro
```

Import fetches every grant currently held by `role` in `database` (for the object types this
resource manages) and populates `grants` with the full result — there is no prior declared list to
diff against. Whenever every object sharing a `(schema, object_type)` currently has the exact same
privileges, import collapses them into one whole-schema entry (`objects` omitted); otherwise it
splits them into one entry per distinct privilege set, with `objects` listing exactly the objects
that share it. The imported resource always starts with `authoritative = false` regardless of the
value you intend to set, since that cannot be inferred from Postgres state; add
`authoritative = true` explicitly in your configuration afterwards if you want full ownership going
forward.
