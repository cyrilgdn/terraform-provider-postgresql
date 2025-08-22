---
layout: "postgresql"
page_title: "PostgreSQL: postgresql_publication"
sidebar_current: "docs-postgresql-resource-postgresql_publication"
description: |-
Creates and manages a publication in a PostgreSQL server database.
---

# postgresql_publication

The `postgresql_publication` resource creates and manages a publication on a PostgreSQL
server.

## Usage

```hcl
resource "postgresql_publication" "publication" {
  name  = "publication"
  tables = ["public.test","another_schema.test"]
}

# Publish all tables in a specific schema
resource "postgresql_publication" "schema_publication" {
  name            = "schema_publication"
  tables_in_schema = "public"
}

# Publish all tables in a schema plus specific tables from other schemas
resource "postgresql_publication" "combined_publication" {
  name            = "combined_publication"
  tables_in_schema = "public"
  tables          = ["another_schema.table1", "another_schema.table2"]
}



## Argument Reference

- `name` - (Required) The name of the publication.
- `database` - (Optional) Which database to create the publication on. Defaults to provider database.
- `tables` - (Optional) Which tables add to the publication. By defaults no tables added. Format of table is `<schema_name>.<table_name>`. If `<schema_name>` is not specified - default database schema will be used.  Table string must be listed in alphabetical order.
- `all_tables` - (Optional) Should be ALL TABLES added to the publication. Defaults to 'false'
- `tables_in_schema` - (Optional) Sets the schema to publish ALL tables from. Conflicts with `all_tables`. Can be used together with `tables` as long as no tables in the `tables` list belong to the schema specified in `tables_in_schema`.
- `owner` - (Optional) Who owns the publication. Defaults to provider user.
- `drop_cascade` - (Optional) Should all subsequent resources of the publication be dropped. Defaults to 'false'
- `publish_param` - (Optional) Which 'publish' options should be turned on. Default to 'insert','update','delete'
- `publish_via_partition_root_param` - (Optional) Should be option 'publish_via_partition_root' be turned on. Default to 'false'

## Notes

- When using `tables_in_schema` together with `tables`, ensure that no tables in the `tables` list belong to the schema specified in `tables_in_schema`. The provider will validate this and return an error if there's a conflict.
- The `tables_in_schema` attribute is equivalent to PostgreSQL's `TABLES IN SCHEMA` syntax and requires PostgreSQL 15 or later.

## Import Example

Publication can be imported using this format:

```
$ terraform import postgresql_publication.publication {{database_name}}.{{publication_name}}
```
