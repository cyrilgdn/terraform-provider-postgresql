---
layout: "postgresql"
page_title: "PostgreSQL: postgresql_schemas"
sidebar_current: "docs-postgresql-data-source-postgresql_schemas"
description: |-
  Retrieves a list of schema names from a PostgreSQL database.
---

# postgresql\_schemas

The ``postgresql_schemas`` data source retrieves a list of schema names from a specified PostgreSQL database.


## Usage

```hcl
data "postgresql_schemas" "my_schemas" {
  database = "my_database"
}

```

## Argument Reference

* `database` - (Required) The PostgreSQL database which will be queried for schema names.
* `include_system_schemas` - (Optional) Determines whether to include system schemas (pg_ prefix and information_schema). 'public' will always be included. Defaults to ``false``.
* `like_pattern` - (Optional) Expression which will be pattern matched in the query using the PostgreSQL ``LIKE`` operator. 
* `not_like_pattern` - (Optional) Expression which will be pattern matched in the query using the PostgreSQL ``NOT LIKE`` operator. 
* `regex_pattern` - (Optional) Expression which will be pattern matched in the query using the PostgreSQL ``~`` (regular expression match) operator.

Note that all 4 optional arguments can be used in conjunction.

## Attributes Reference

* `schemas` - A list of full names of found schemas.
