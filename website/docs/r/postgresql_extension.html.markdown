---
layout: "postgresql"
page_title: "PostgreSQL: postgresql_extension"
sidebar_current: "docs-postgresql-resource-postgresql_extension"
description: |-
  Creates and manages an extension on a PostgreSQL server.
---

# postgresql\_extension

The ``postgresql_extension`` resource creates and manages an extension on a PostgreSQL
server.


## Usage

```hcl
resource "postgresql_extension" "my_extension" {
  name = "pg_trgm"
}
```

## Argument Reference

* `name` - (Required) The name of the extension.
* `schema` - (Optional) Sets the schema of an extension.
* `version` - (Optional) Sets the version number of the extension.
* `database` - (Optional) Which database to create the extension on. Defaults to provider database.
* `drop_cascade` - (Optional) When true, will also drop all the objects that depend on the extension, and in turn all objects that depend on those objects. (Default: false)
* `create_cascade` - (Optional) When true, will also create any extensions that this extension depends on that are not already installed. (Default: false)

## Import

PostgreSQL Extensions can be imported using the database name and the extension's resource name, e.g.

`terraform import postgresql_extension.uuid_ossp example-database.uuid-ossp`
