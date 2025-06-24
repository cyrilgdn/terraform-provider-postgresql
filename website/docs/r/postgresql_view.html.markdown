---
layout: "postgresql"
page_title: "PostgreSQL: postgresql_view"
sidebar_current: "docs-postgresql-resource-postgresql_view"
description: |-
Creates and manages a view on a PostgreSQL server.
---

# postgresql_view

The `postgresql_view` resource creates and manages a view on a PostgreSQL server.

## Usage

```hcl
resource "postgresql_view" "aggregation_view" {
    database  = "database_name"
    schema = "schema_name"
    name = "aggregation_view"
    query = <<-EOF
      SELECT schemaname, tablename
      FROM pg_catalog.pg_tables;
    EOF
    with_check_option = "CASCADED"
    with_security_barrier = true
    with_security_invoker = true
    drop_cascade = true
}
```

## Argument Reference

- `database` - (Optional) The database where the view is located.
  If not specified, the view is created in the current database.

- `schema` - (Optional) The schema where the view is located.
  If not specified, the view is created in the current schema.

- `name` - (Required) The name of the view.

- `query` - (Required) The query.

- `with_check_option` - (Optional) The check option which controls the behavior
  of automatically updatable views. One of: CASCADED, LOCAL. Default is not set.

- `with_security_barrier` - (Optional) This should be used if the view is intended to provide row-level security. Default is false.

- `with_security_invoker` - (Optional) This option causes the underlying base relations to be
  checked against the privileges of the user of the view rather than the view owner. Default is false.

- `drop_cascade` - (Optional) - True tp automatically drop objects that depend on the view (such as other views),
  and in turn all objects that depend on those objects. Default is false.

## Postgres documentation

- https://www.postgresql.org/docs/16/sql-createview.html
