---
layout: "postgresql"
page_title: "PostgreSQL: postgresql_tables"
sidebar_current: "docs-postgresql-data-source-postgresql_tables"
description: |-
  Retrieves a list of table names from a PostgreSQL database.
---

# postgresql\_connection

The ``postgresql_connection`` data source retrieves a current config of PostgreSQL connection.


## Usage

```hcl
data "postgresql_connection" "current" {
}
```

## Argument Reference

There are no arguments available for this data source.

## Attributes Reference

* `host` - The current connected PostgreSQL server hostname
* `port` - The current connected PostgreSQL server port
* `scheme` - TThe current connected PostgreSQL server scheme
* `username` - The current connected username of the PostgreSQL server
* `database_username` - The current connected username of the PostgreSQL server
* `version` - The current connected PostgreSQL server version
* `database` - The current connected PostgreSQL server database