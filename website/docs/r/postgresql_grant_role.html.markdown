---
layout: "postgresql"
page_title: "PostgreSQL: postgresql_grant_role"
sidebar_current: "docs-postgresql-resource-postgresql_grant_role"
description: |-
  Creates and manages membership in a role to one or more other roles.
---

# postgresql\_grant\_role

The ``postgresql_grant_role`` resource creates and manages membership in a role to one or more other roles.

~> **Note:** This resource needs PostgreSQL version 9 or above.

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
