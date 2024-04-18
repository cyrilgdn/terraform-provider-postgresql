---
layout: "postgresql"
page_title: "PostgreSQL: postgresql_grant"
sidebar_current: "docs-postgresql-resource-postgresql_grant"
description: |-
  Creates and manages privileges given to a user for a database schema.
---

# postgresql\_security\_label

The ``postgresql_security_label`` resource creates and manages security labels.

See [PostgreSQL documentation](https://www.postgresql.org/docs/current/sql-security-label.html)

~> **Note:** This resource needs Postgresql version 11 or above.

## Usage

```hcl
resource "postgresql_role" "my_role" {
  name  = "my_role"
  login = true
}

resource "postgresql_security_label" "workload" {
  object_type    = "role"
  object_name    = postgresql_role.my_role.name
  label_provider = "pgaadauth"
  label          = "aadauth,oid=00000000-0000-0000-0000-000000000000,type=service"
}
```

## Argument Reference

* `object_type` - (Required) The PostgreSQL object type to apply this security label to.
* `object_name` - (Required) The name of the object to be labeled. Names of objects that reside in schemas (tables, functions, etc.) can be schema-qualified.
* `label_provider` - (Required) The name of the provider with which this label is to be associated.
* `label` - (Required) The value of the security label.
