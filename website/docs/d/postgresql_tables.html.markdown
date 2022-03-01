---
layout: "postgresql"
page_title: "PostgreSQL: postgresql_tables"
sidebar_current: "docs-postgresql-data-source-postgresql_tables"
description: |-
  Retrieves a list of table names from a PostgreSQL database.
---

# postgresql\_tables

The ``postgresql_tables`` data source retrieves a list of table names from a specified PostgreSQL database.


## Usage

```hcl
data "postgresql_tables" "my_tables" {
  database = "my_database"
}

```

## Argument Reference

* `database` - (Required) The PostgreSQL database which will be queried for table names.
* `schemas` - (Optional) List of PostgreSQL schema(s) which will be queried for table names. Queries all schemas in the database by default.
* `table_types` - (Optional) List of PostgreSQL table types which will be queried for table names. Includes all table types by default (including views and temp tables). Use 'BASE TABLE' for normal tables only.
* `like_any_patterns` - (Optional) List of expressions which will be pattern matched against table names in the query using the PostgreSQL ``LIKE ANY`` operators. 
* `like_all_patterns` - (Optional) List of expressions which will be pattern matched against table names in the query using the PostgreSQL ``LIKE ALL`` operators. 
* `not_like_all_patterns` - (Optional) List of expressions which will be pattern matched against table names in the query using the PostgreSQL ``NOT LIKE ALL`` operators. 
* `regex_pattern` - (Optional) Expression which will be pattern matched against table names in the query using the PostgreSQL ``~`` (regular expression match) operator.

Note that all optional arguments can be used in conjunction.

## Attributes Reference

* `tables` - The list of PostgreSQL tables retrieved by this data source. Note that this returns a set, so duplicate table names across different schemas will be consolidated.
