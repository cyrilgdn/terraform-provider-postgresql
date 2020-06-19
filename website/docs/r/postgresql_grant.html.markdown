---
layout: "postgresql"
page_title: "PostgreSQL: postgresql_grant"
sidebar_current: "docs-postgresql-resource-postgresql_grant"
description: |-
  Creates and manages privileges given to a user for a database schema.
---

# postgresql\_grant

The ``postgresql_grant`` resource creates and manages privileges given to a user for a database schema.

~> **Note:** This resource needs Postgresql version 9 or above.

## Usage

```hcl
resource "postgresql_grant" "readonly_tables" {
  database    = "test_db"
  role        = "test_role"
  schema      = "public"
  object_type = "table"
  privileges  = ["SELECT"]
}
```

## Argument Reference

* `role` - (Required) The name of the role to grant privileges on.
* `database` - (Required) The database to grant privileges on for this role.
* `schema` - (Required) The database schema to grant privileges on for this role.
* `object_type` - (Required) The PostgreSQL object type to grant the privileges on (one of: table, sequence,function).
* `privileges` - (Required) The list of privileges to grant.
