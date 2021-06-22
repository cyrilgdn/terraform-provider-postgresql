---
layout: "postgresql"
page_title: "PostgreSQL: postgresql_grant_resource"
sidebar_current: "docs-postgresql-resource-postgresql_grant_resource"
description: |-
  Creates and manages privileges given to a user for specific resources.
---

# postgresql\_grant\_resource

The ``postgresql_grant_resource`` resource creates and manages privileges given to a user for specific resources.

See [PostgreSQL documentation](https://www.postgresql.org/docs/current/sql-grant.html)

~> **Note:** This resource needs Postgresql version 9 or above.

## Usage

```hcl
resource "postgresql_grant_resource" "readonly_tables" {
  database    = "test_db"
  role        = "test_role"
  schema      = "public"
  object_type = "table"
  privileges  = ["SELECT"]
  resources   = ["table_name1", "table_name2"]
}
```

## Argument Reference

* `role` - (Required) The name of the role to grant privileges on, Set it to "public" for all roles.
* `database` - (Required) The database to grant privileges on for this role.
* `schema` - The database schema to grant privileges on for this role (Required except if object_type is "database")
* `object_type` - (Required) The PostgreSQL object type to grant the privileges on (one of: `table`, `sequence`, `function`).
* `privileges` - (Required) The list of privileges to grant. There are different kinds of privileges: `SELECT`, `INSERT`, `UPDATE`, `DELETE`, `TRUNCATE`, `REFERENCES`, `TRIGGER`, `TEMPORARY`, `EXECUTE`, and `USAGE`. An empty list could be provided to revoke all privileges for this role.
* `resources` - (Required) The list of resources (e.g. table name, sequence name or function name) on which the grant should be applied on.


## Examples

Gives insert access to a specific table:

```hcl
resource "postgresql_grant" "allow_create_user" {
  database    = "test_db"
  role        = "public"
  schema      = "public"
  object_type = "table"
  privileges  = ["INSERT"]
  resources   = ["user"]
}
```

Gives access to a few utility functions:

```hcl
resource "postgresql_grant" "allow_utility_functions" {
  database    = "test_db"
  role        = "public"
  schema      = "public"
  object_type = "function"
  privileges  = ["EXECUTE"]
  resources   = ["fetch_user", "fetch_object"]
}
```
