---
layout: "postgresql"
page_title: "PostgreSQL: postgresql_physical_replication_slot"
sidebar_current: "docs-postgresql-resource-postgresql_physical_replication_slot"
description: |-
Creates and manages a physical replication slot on a PostgreSQL server.
---

# postgresql\_physical\_replication\_slot

The ``postgresql_physical_replication_slot`` resource creates and manages a physical replication slot on a PostgreSQL
server. This is useful to setup a cross datacenter replication, with Patroni for example, or permit
any stand-by cluster to replicate physically data.


## Usage

```hcl
resource "postgresql_physical_replication_slot" "my_slot" {
  name  = "my_slot"
}

## Argument Reference

* `name` - (Required) The name of the replication slot.
