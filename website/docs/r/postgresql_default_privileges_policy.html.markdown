---
layout: "postgresql"
page_title: "PostgreSQL: postgresql_default_privileges_policy"
sidebar_current: "docs-postgresql-resource-postgresql_default_privileges_policy"
description: |-
  Creates and manages, in a single aggregated resource, the default privileges given by an owner for a database.
---

# postgresql\_default\_privileges\_policy

The ``postgresql_default_privileges_policy`` resource manages, as a single unit, all the default
privileges granted by an `owner` in a `database`.

It is the coarse-grained counterpart of
[`postgresql_default_privileges`](postgresql_default_privileges.html): instead of one Terraform
resource per `(role, schema, object_type)` tuple, a single `postgresql_default_privileges_policy`
resource owns every default-privilege grant for a given `(database, owner)` pair. This avoids the
parallel `ALTER DEFAULT PRIVILEGES` statements issued by many independent
`postgresql_default_privileges` resources, which can deadlock against each other or leave a
partially-applied (tainted) state when Terraform applies them concurrently. Because every grant for
the owner is reconciled inside a single transaction, this resource does not need `depends_on` chains
between grants to serialize them.

~> **Note:** This resource needs Postgresql version 9 or above.

~> **Warning: `authoritative = true`.** When `authoritative` is `true`, this resource takes full
ownership of every default privilege granted by `owner` in `database`, for the object types it
manages (table, sequence, function, type, schema). On **every apply**, and on **destroy**, it
**revokes** any such default privilege found in Postgres that is not declared in `grants` — including
ones created outside Terraform (manually, by another tool, or by a `postgresql_default_privileges`
resource). Do not mix `authoritative = true` with `postgresql_default_privileges`, or manual
`ALTER DEFAULT PRIVILEGES`, for the same `(database, owner)` pair, or those grants will be revoked on
the next apply. `authoritative` defaults to `false` precisely so that adopting this resource against
an existing production database cannot silently revoke privileges you didn't intend to touch; opt
into `authoritative = true` only once you are certain `grants` fully describes the desired state.

## Usage

```hcl
resource "postgresql_default_privileges_policy" "developers_product_service" {
  database = "product_service"
  owner    = "developers"

  # If true, any default privileges in PG not defined in this block will be revoked.
  # If false, it only ensures defined grants are present (additive mode).
  authoritative = true

  grants = [
    {
      schema            = "offer" # Optional. If omitted, applies globally to the database (schema = null)
      role              = "product_service_role_ro"
      object_type       = "table"
      privileges        = ["SELECT"]
      with_grant_option = false
    }
  ]
}
```

## Argument Reference

* `database` - (Required, Forces new resource) The database to manage default privileges for this owner.
* `owner` - (Required, Forces new resource) The role that creates objects for which the default privileges are managed.
* `authoritative` - (Optional) Defaults to `false`. If `true`, every default-privilege grant found for
  `owner` in `database` that is not declared in `grants` (for the object types managed by this
  resource: table, sequence, function, type, schema) is revoked on apply and on destroy — see the
  warning above. If `false` (the safe default), the resource only ensures the grants declared in
  `grants` are present (additive mode); it never revokes grants it did not declare, on apply or on
  destroy.
* `grants` - (Required) The list of default privilege grants to apply for this `(database, owner)`
  policy. Each entry supports:
  * `schema` - (Optional) The schema to scope this grant to. If omitted, the grant applies globally
    to future objects created in any schema of the database.
  * `role` - (Required) The role that will automatically be granted the specified privileges on new
    objects created by the owner.
  * `object_type` - (Required) The PostgreSQL object type to set the default privileges on (one of:
    table, sequence, function, type, schema).
  * `privileges` - (Required) List of privileges (e.g., SELECT, INSERT, UPDATE, DELETE) to grant on
    new objects created by the owner. An empty list revokes all default privileges for this entry.
  * `with_grant_option` - (Optional) Defaults to `false`. Permits the grant recipient to grant it to
    others. Cannot be `true` when `role` is `public`.

Each `(schema, object_type, role)` combination must appear at most once across `grants`.

## Attributes Reference

In addition to the arguments above, the following attribute is exported:

* `id` - The ID of the resource, computed as `<database>_<owner>`.

## Import

`postgresql_default_privileges_policy` can be imported using `<database>/<owner>`, e.g.

```
terraform import postgresql_default_privileges_policy.developers_product_service product_service/developers
```

Import fetches every default privilege currently recorded by Postgres for `owner` in `database` (for
the object types this resource manages) and populates `grants` with the full result — there is no
prior declared list to diff against. The imported resource always starts with `authoritative = false`
regardless of the value you intend to set, since that cannot be inferred from Postgres state; add
`authoritative = true` explicitly in your configuration afterwards if you want full ownership going
forward.
