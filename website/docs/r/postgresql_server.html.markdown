---
layout: "postgresql"
page_title: "PostgreSQL: postgresql_server"
sidebar_current: "docs-postgresql-resource-postgresql_server"
description: |-
  Creates and manages a foreign server on a PostgreSQL server.
---

# postgresql\_server

The ``postgresql_server`` resource creates and manages a foreign server on a PostgreSQL server.


## Usage

```hcl
resource "postgresql_extension" "ext_postgres_fdw" {
  name = "postgres_fdw"
}

resource "postgresql_server" "myserver_postgres" {
  server_name = "myserver_postgres"
  fdw_name    = "postgres_fdw"
  options = {
    host   = "foo"
    dbname = "foodb"
    port   = "5432"
  }

  depends_on = [postgresql_extension.ext_postgres_fdw]
}
```

```hcl
resource "postgresql_extension" "ext_file_fdw" {
  name = "file_fdw"
}

resource "postgresql_server" "myserver_file" {
  server_name = "myserver_file"
  fdw_name    = "file_fdw"  
  depends_on = [postgresql_extension.ext_file_fdw]
}
```

## Argument Reference

* `server_name` - (Required) The name of the foreign server to be created.
* `fdw_name` - (Required) The name of the foreign-data wrapper that manages the server.
Changing this value
  will force the creation of a new resource as this value can only be set
  when the foreign server is created.
* `options` - (Optional) This clause specifies the options for the server. The options typically define the connection details of the server, but the actual names and values are dependent on the server's foreign-data wrapper.
* `server_type` - (Optional) Optional server type, potentially useful to foreign-data wrappers.
Changing this value
  will force the creation of a new resource as this value can only be set
  when the foreign server is created.
* `server_version` - (Optional) Optional server version, potentially useful to foreign-data wrappers.
* `server_owner` - (Optional) By default, the user who defines the server becomes its owner. Set this value to configure the new owner of the foreign server.
* `drop_cascade` - (Optional) When true, will drop objects that depend on the server (such as user mappings), and in turn all objects that depend on those objects . (Default: false)
