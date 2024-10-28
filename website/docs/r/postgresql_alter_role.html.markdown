---
layout: "postgresql"
page_title: "PostgreSQL: postgresql_alter_role"
sidebar_current: "docs-postgresql-resource-postgresql_alter_role"
description: |-
  Creates and manages the attributes of a role.
---

# postgresql\_alter\_role

The ``postgresql_alter_role`` resource creates and manages the attributes of a role.

See [PostgreSQL documentation](https://www.postgresql.org/docs/current/sql-alterrole.html)

~> **Note:** This resource needs Postgresql version 10 or above.

## Usage

```hcl
resource "postgresql_alter_role" "set_pgaudit_logging" {
  role            = "test_role"
  parameter_key   = "pgaudit.log"
  parameter_value = "ALL"
}
```

## Argument Reference

* `role` - (Required) The name of the role to alter the attributes of.
* `parameter_key` - (Required) The configuration parameter to be set.
* `parameter_value` - (Required) The value for the configuration parameter.

## Example

```hcl
resource "postgresql_role" "pgaudit_role" {
  name = "test_pgaudit_role"
}

resource "postgresql_alter_role" "set_pgaudit_logging" {
  role            = postgresql_role.pgaudit_role.name
  parameter_key   = "pgaudit.log"
  parameter_value = "ALL"
}
```