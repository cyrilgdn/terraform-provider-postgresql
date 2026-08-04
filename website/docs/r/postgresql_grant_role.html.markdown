---
layout: "postgresql"
page_title: "PostgreSQL: postgresql_grant_role"
sidebar_current: "docs-postgresql-resource-postgresql_grant_role"
description: |-
  Creates and manages membership in a role to one or more other roles.
---

# postgresql\_grant\_role

The ``postgresql_grant_role`` resource creates and manages membership in a role to one or more other roles in a non-authoritative way.

When using ``postgresql_grant_role`` resource it is likely because the PostgreSQL role you are modifying was created outside of this provider.

~> **Note:** This resource needs PostgreSQL version 9 or above. The `with_inherit_option` and `with_set_option` arguments require PostgreSQL 16 or above.

## Usage

```hcl
resource "postgresql_grant_role" "grant_root" {
  role              = "root"
  grant_role        = "application"
  with_admin_option = true
}
```

```hcl
resource "postgresql_grant_role" "grant_without_inherit" {
  role                = "app_user"
  grant_role          = "app_role"
  with_inherit_option = false
}
```

~> **Note:** If you use `postgresql_grant_role` for a role that you also manage with a `postgresql_role` resource, you need to ignore the changes of the `roles` attribute in the `postgresql_role` resource or they will fight over what your role grants should be. e.g.:
```hcl
resource "postgresql_role" "bob" {
  name = "bob"

  lifecycle {
    ignore_changes = [
      roles,
    ]
  }
}

resource "postgresql_grant_role" "bob_admin" {
  role       = "bob"
  grant_role = "admin"
}
```

## Argument Reference

* `role` - (Required) The name of the role that is granted a new membership.
* `grant_role` - (Required) The name of the role that is added to `role`.
* `with_admin_option` - (Optional) Giving ability to grant membership to others or not for `role`. (Default: false)
* `with_inherit_option` - (Optional) Sets the `INHERIT` option for the membership (PostgreSQL 16+). When set, emits `WITH INHERIT TRUE` or `WITH INHERIT FALSE`. When omitted, PostgreSQL follows the member role's `INHERIT` attribute.
* `with_set_option` - (Optional) Sets the `SET` option for the membership (PostgreSQL 16+). When set, emits `WITH SET TRUE` or `WITH SET FALSE`. When omitted, PostgreSQL defaults to `SET true`.
