---
layout: "postgresql"
page_title: "PostgreSQL: postgresql_permissions"
sidebar_current: "docs-postgresql-resource-postgresql_permissions"
description: |-
  Creates and manages permissions for a role
---

# postgresql\_permissions

The ``postgresql_permissions`` resource creates and manages permissions for a role in a non-authoritative way. This resource is useful when you want to manage permissions for a role that was created outside of this provider.

When using ``postgresql_permissions`` resource it is likely because the PostgreSQL role you are modifying was created outside of this provider.

~> **Note:** This resource needs PostgreSQL version 9 or above.

## Usage

```hcl
resource "postgresql_permissions" "create_db_permissions" {
  role              = "root"
  create_db = true
  create_role = true
}
```

## Notes

If the role already has the permission, the resource will not modify the role but will begin to manage the roles permissions.

## Argument Reference

* `role` - (Required) The name of the role that is granted the permissions.
* `create_db` - (Optional) grants the `role` `CREATEDB` permission. (Default: false)
* `create_role` - (Optional) grants the `role` `CREATEROLE` permission. (Default: false)
