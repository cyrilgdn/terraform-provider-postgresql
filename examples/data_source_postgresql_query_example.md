# PostgreSQL Query Data Source Example

This example demonstrates how to use the `postgresql_query` data source to execute queries and access the results.

## Basic Usage

```hcl
# Query with literal values
data "postgresql_query" "version" {
  database = "mydb"
  query    = "SELECT version() as pg_version, current_database() as db_name"
}

# Access the results
output "postgres_version" {
  value = data.postgresql_query.version.rows[0].pg_version
}

output "database_name" {
  value = data.postgresql_query.version.rows[0].db_name
}
```

## Query with Parameters

```hcl
# Query with arguments (using $1, $2, etc. placeholders)
data "postgresql_query" "user_tables" {
  database = "mydb"
  query    = "SELECT schemaname, tablename FROM pg_tables WHERE schemaname = $1"
  args     = ["public"]
}

# Iterate over results
output "public_tables" {
  value = [
    for row in data.postgresql_query.user_tables.rows : 
    "${row.schemaname}.${row.tablename}"
  ]
}
```

## Available Attributes

- `rows` - List of maps containing row data, where keys are column names
- `columns` - List of column metadata with `name` and `type` attributes