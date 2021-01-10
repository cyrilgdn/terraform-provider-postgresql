---
layout: "postgresql"
page_title: "PostgreSQL: postgresql_grant"
sidebar_current: "docs-postgresql-resource-postgresql_grant"
description: |-
  Creates and manages privileges given to a user for a database schema.
---

# postgresql\_grant

The ``postgresql_grant`` resource creates and manages privileges given to a user for a database schema.

See [PostgreSQL documentation](https://www.postgresql.org/docs/current/sql-grant.html)

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

* `role` - (Required) The name of the role to grant privileges on, Set it to "public" for all roles.
* `database` - (Required) The database to grant privileges on for this role.
* `schema` - The database schema to grant privileges on for this role (Required except if object_type is "database")
* `object_type` - (Required) The PostgreSQL object type to grant the privileges on (one of: database, schema, table, sequence,function).
* `privileges` - (Required) The list of privileges to grant. There are different kinds of privileges: SELECT, INSERT, UPDATE, DELETE, TRUNCATE, REFERENCES, TRIGGER, CREATE, CONNECT, TEMPORARY, EXECUTE, and USAGE. An empty list could be provided to revoke all privileges for this role.


## Examples

Revoke default accesses for public schema:

```hcl
resource "postgresql_grant" "revoke_public" {
  database    = "test_db"
  role        = "public"
  schema      = "public"
  object_type = "schema"
  privileges  = []
}
```
