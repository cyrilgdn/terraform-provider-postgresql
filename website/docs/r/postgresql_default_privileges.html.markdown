---
layout: "postgresql"
page_title: "PostgreSQL: postgresql_default_privileges"
sidebar_current: "docs-postgresql-resource-postgresql_default_privileges"
description: |-
  Creates and manages default privileges given to a user for a database schema.
---

# postgresql\_default\_privileges

The ``postgresql_default_privileges`` resource creates and manages default privileges given to a user for a database schema.

~> **Note:** This resource needs Postgresql version 9 or above.

## Usage

```hcl
resource "postgresql_default_privileges" "read_only_tables" {
  role     = "test_role"
  database = "test_db"
  schema   = "public"

  owner       = "db_owner"
  object_type = "table"
  privileges  = ["SELECT"]
}
```

## Argument Reference

* `role` - (Required) The name of the role to which grant default privileges on.
* `database` - (Required) The database to grant default privileges for this role.
* `owner` - (Required) Role for which apply default privileges (You can change default privileges only for objects that will be created by yourself or by roles that you are a member of).
* `schema` - (Optional) The database schema to set default privileges for this role.
* `object_type` - (Required) The PostgreSQL object type to set the default privileges on (one of: table, sequence, function, type, schema).
* `privileges` - (Required) The list of privileges to apply as default privileges. An empty list could be provided to revoke all default privileges for this role.


## Examples

Revoke default privileges for functions for "public" role:

```hcl
resource "postgresql_default_privileges" "revoke_public" {
  database    = postgresql_database.example_db.name
  role        = "public"
  owner       = "object_owner"
  object_type = "function"
  privileges  = []
}
```
