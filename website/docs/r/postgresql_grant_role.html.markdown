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

~> **Note:** This resource needs PostgreSQL version 9 or above.

~> **Note:** `postgresql_grant_role` **cannot** be used in conjunction with `postgresql_role` or they will fight over what your role grants should be.

## Usage

```hcl
resource "postgresql_grant_role" "grant_root" {
  role              = "root"
  grant_role        = "application"
  with_admin_option = true
}
```

## Argument Reference

* `role` - (Required) The name of the role that is granted a new membership.
* `grant_role` - (Required) The name of the role that is added to `role`.
* `with_admin_option` - (Optional) Giving ability to grant membership to others or not for `role`. (Default: false)
