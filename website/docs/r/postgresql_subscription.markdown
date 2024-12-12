---
layout: "postgresql"
page_title: "PostgreSQL: postgresql_subscription"
sidebar_current: "docs-postgresql-resource-postgresql_subscription"
description: |-
Creates and manages a subscription in a PostgreSQL server database.
---

# postgresql_subscription

The `postgresql_subscription` resource creates and manages a publication on a PostgreSQL
server.

## Usage

```hcl
resource "postgresql_subscription" "subscription" {
  name          = "subscription"
  conninfo      = "host=localhost port=5432 dbname=mydb user=postgres password=postgres"
  publications  = ["publication"]
}
```

## Argument Reference

- `name` - (Required) The name of the publication.
- `conninfo` - (Required) The connection string to the publisher. It should follow the [keyword/value format](https://www.postgresql.org/docs/current/libpq-connect.html#LIBPQ-CONNSTRING)
- `publications` - (Required) Names of the publications on the publisher to subscribe to
- `database` - (Optional) Which database to create the subscription on. Defaults to provider database.
- `create_slot` - (Optional) Specifies whether the command should create the replication slot on the publisher. Default behavior is true
- `slot_name` - (Optional) Name of the replication slot to use. The default behavior is to use the name of the subscription for the slot name

## Postgres documentation
- https://www.postgresql.org/docs/current/sql-createsubscription.html