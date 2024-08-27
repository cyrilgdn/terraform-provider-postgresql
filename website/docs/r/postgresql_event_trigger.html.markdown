---
layout: "postgresql"
page_title: "PostgreSQL: postgresql_event_trigger"
sidebar_current: "docs-postgresql-resource-postgresql_event_trigger"
description: |-
  Creates and manages an event trigger on a PostgreSQL server.
---

# postgresql\_event_trigger

The ``postgresql_event_trigger`` resource creates and manages [event trigger
objects](https://www.postgresql.org/docs/current/static/event-triggers.html)
within a PostgreSQL server instance.

## Usage

```hcl
resource "postgresql_function" "function" {
    name = "test_function"

    returns = "event_trigger"
    language = "plpgsql"
    body = <<-EOF
        BEGIN
            RAISE EXCEPTION 'command % is disabled', tg_tag;
        END;
    EOF
}

resource "postgresql_event_trigger" "event_trigger" {
  name = "event_trigger_test"
  function = postgresql_function.function.name
  on = "ddl_command_start"
  owner = "postgres"

  filter {
    variable = "TAG"
    values = [
      "DROP TABLE"
    ]
  }
}
```

## Argument Reference

* `name` - (Required) The name of the event trigger.

* `on` - (Required) The name of the on event the trigger will listen to. The allowed names are "ddl_command_start", "ddl_command_end", "sql_drop" or "table_rewrite".

* `function` - (Required) A function that is declared as taking no argument and returning type event_trigger.

* `filter` - (Optional) Lists of filter variables to restrict the firing of the trigger.  Currently the only supported filter_variable is TAG.
  * `variable` - (Required) The name of a variable used to filter events. Currently the only supported value is TAG.
  * `values` - (Required) The name of the filter variable name. For TAG, this means a list of command tags (e.g., 'DROP FUNCTION').

* `database` - (Optional) The database where the event trigger is located.
  If not specified, the function is created in the current database.

* `schema` - (Optional) Schema where the function is located.
  If not specified, the function is created in the current schema.

* `status` - (Optional) These configure the firing of event triggers. The allowed names are "disable", "enable", "enable_replica" or "enable_always". Default is "enable".

* `owner` - (Required) The user name of the owner of the event trigger. You can't use 'current_role', 'current_user' or 'session_user' in order to avoid drifts.

## Import Example

It is possible to import a `postgresql_event_trigger` resource with the following
command:

```
$ terraform import postgresql_event_trigger.event_trigger_test "database.event_trigger"
```
