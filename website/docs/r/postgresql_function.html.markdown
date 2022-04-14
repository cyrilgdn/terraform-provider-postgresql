---
layout: "postgresql"
page_title: "PostgreSQL: postgresql_function"
sidebar_current: "docs-postgresql-resource-postgresql_function"
description: |-
Creates and manages a function on a PostgreSQL server.
---

# postgresql\_function

The ``postgresql_function`` resource creates and manages a function on a PostgreSQL
server.

## Usage

```hcl
resource "postgresql_function" "increment" {
    name = "increment"
    args = [
        {
            name = "i"
            type = "integer"
        }
    ]
    returns = "integer"
    body = <<-EOF
        AS $$
        BEGIN
            RETURN i + 1;
        END;
        $$ LANGUAGE plpgsql;
    EOF
}
```

## Argument Reference

* `name` - (Required) The name of the function.

* `schema` - (Optional) The schema where the function is located.
  If not specified, the function is created in the current schema.

* `args` - (Optional) List of arguments for the function.

* `returns` - (Optional) Type that the function returns.

* `body` - (Required) Function body.
  This should be everything after the return type in the function definition.

* `drop_cascade` - (Optional) True to automatically drop objects that depend on the function (such as 
  operators or triggers), and in turn all objects that depend on those objects.

The `args` list element:

* `type` - (Required) The type of the argument.

* `name` - (Optional) The name of the argument.

* `mode` - (Optional) Can be one of IN, INOUT, OUT, or VARIADIC. Default is IN.

* `default` - (Optional) An expression to be used as default value if the parameter is not specified.