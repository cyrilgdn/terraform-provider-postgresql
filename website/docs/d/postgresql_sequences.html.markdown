---
layout: "postgresql"
page_title: "PostgreSQL: postgresql_sequences"
sidebar_current: "docs-postgresql-data-source-postgresql_sequences"
description: |-
  Retrieves a list of sequence names from a PostgreSQL database.
---

# postgresql\_sequences

The ``postgresql_sequences`` data source retrieves a list of sequence names from a specified PostgreSQL database.


## Usage

```hcl
data "postgresql_sequences" "my_sequences" {
  database = "my_database"
}

```

## Argument Reference

* `database` - (Required) The PostgreSQL database which will be queried for sequence names.
* `schemas` - (Optional) List of PostgreSQL schema(s) which will be queried for sequence names. Queries all schemas in the database by default.
* `data_types` - (Optional) List of PostgreSQL sequence data types which will be queried for sequence names. Includes all data types by default.
* `like_any_patterns` - (Optional) List of expressions which will be pattern matched against sequence names in the query using the PostgreSQL ``LIKE ANY`` operators. 
* `like_all_patterns` - (Optional) List of expressions which will be pattern matched against sequence names in the query using the PostgreSQL ``LIKE ALL`` operators. 
* `not_like_all_patterns` - (Optional) List of expressions which will be pattern matched against sequence names in the query using the PostgreSQL ``NOT LIKE ALL`` operators. 
* `regex_pattern` - (Optional) Expression which will be pattern matched against sequence names in the query using the PostgreSQL ``~`` (regular expression match) operator.

Note that all optional arguments can be used in conjunction.

## Attributes Reference

* `sequences` - A list of PostgreSQL sequences retrieved by this data source. Each sequence consists of the fields documented below.
___

The `sequence` block consists of: 

* `object_name` - The sequence name.

* `schema_name` - The parent schema.

* `data_type` - The sequence's data type as defined in ``information_schema.sequences``.
