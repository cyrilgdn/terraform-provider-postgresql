# This tests reproduces an issue for the following error message.
# ```
# terraform.tfstate
# ╷
# │ Error: could not execute revoke query: pq: tuple concurrently updated
# │
# │   with postgresql_grant.public_revoke_database["test3"],
# │   on test.tf line 40, in resource "postgresql_grant" "public_revoke_database":
# │   40: resource "postgresql_grant" "public_revoke_database" {
# │
# ╵
# ```

terraform {
  required_version = ">= 1.0"

  required_providers {
    postgresql = {
      source  = "rlmartin/postgresql"
      version = ">=1.14"
    }
  }
}

locals {
  databases = toset([for idx in range(4) : format("test%d", idx)])
}

provider "postgresql" {
  superuser = false
}

resource "postgresql_database" "db" {
  for_each = local.databases
  name     = each.key

  # Use template1 instead of template0 (see https://www.postgresql.org/docs/current/manage-ag-templatedbs.html)
  template = "template1"
}

resource "postgresql_role" "demo" {
  name     = "demo"
  login    = true
  password = "Happy-Holidays!"
}

locals {
  # Create a local that is depends on postgresql_database to ensure it's created
  dbs = { for database in local.databases : database => postgresql_database.db[database].name }
}

# Revoke default accesses for PUBLIC role to the databases
resource "postgresql_grant" "public_revoke_database" {
  for_each    = local.dbs
  database    = each.value
  role        = "public"
  object_type = "database"
  privileges  = []

  with_grant_option = true
}

# Revoke default accesses for PUBLIC role to the public schema
resource "postgresql_grant" "public_revoke_schema" {
  for_each    = local.dbs
  database    = each.value
  role        = "public"
  schema      = "public"
  object_type = "schema"
  privileges  = []

  with_grant_option = true
}

resource "postgresql_grant" "demo_db_connect" {
  for_each    = local.dbs
  database    = each.value
  role        = postgresql_role.demo.name
  schema      = "public"
  object_type = "database"
  privileges  = ["CONNECT"]
}
