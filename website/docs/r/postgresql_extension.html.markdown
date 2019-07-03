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
