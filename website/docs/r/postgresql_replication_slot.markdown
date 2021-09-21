---
layout: "postgresql"
page_title: "PostgreSQL: postgresql_replication_slot"
sidebar_current: "docs-postgresql-resource-postgresql_replication_slot"
description: |-
Creates and manages a replication slot on a PostgreSQL server.
---

# postgresql\_replication\_slot

The ``postgresql_replication_slot`` resource creates and manages a replication slot on a PostgreSQL
server.


## Usage

```hcl
resource "postgresql_replication_slot" "my_slot" {
  name  = "my_slot"
  plugin = "test_decoding"
}
```

## Argument Reference

* `name` - (Required) The name of the replication slot.
* `plugin` - (Required) Sets the output plugin.
* `database` - (Optional) Which database to create the replication slot on. Defaults to provider database.
