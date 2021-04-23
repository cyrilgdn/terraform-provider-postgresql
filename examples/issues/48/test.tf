terraform {
  required_version = ">= 0.14"
  required_providers {
    postgresql = {
      source  = "cyrilgdn/postgresql"
      version = ">=1.12"
    }
  }
}

resource "postgresql_role" "owner" {
  for_each = toset([for idx in range(local.nb_dabatases) : format("test_db%d", idx)])
  name     = each.key
}

resource "postgresql_database" "db" {
  depends_on = [
    postgresql_role.owner,
  ]
  for_each = toset([for idx in range(local.nb_dabatases) : format("test_db%d", idx)])
  name     = each.key
  owner    = each.key
}

resource "postgresql_role" "role" {
  for_each = toset([for idx in range(local.nb_roles) : format("test_role%d", idx)])
  name     = each.key
}

resource "postgresql_grant" "grant" {
  depends_on = [
    postgresql_database.db,
    postgresql_role.role
  ]

  for_each = { for idx in range(local.nb_roles) : idx => format("test_role%d", idx) }

  role        = each.value
  database    = format("test_db%d", each.key % local.nb_dabatases)
  schema      = "public"
  object_type = "table"
  privileges  = ["SELECT"]
}

resource "postgresql_default_privileges" "dp" {
  depends_on = [
    postgresql_database.db,
    postgresql_role.role,
  ]

  for_each = { for idx in range(local.nb_roles) : idx => format("test_role%d", idx) }

  role        = each.value
  database    = format("test_db%d", each.key % local.nb_dabatases)
  owner       = format("test_db%d", each.key % local.nb_dabatases)
  schema      = "public"
  object_type = "table"
  privileges  = ["SELECT"]
}

locals {
  nb_dabatases = 3
  nb_roles     = 15 * local.nb_dabatases
}

module "test" {
  for_each = toset([for idx in range(20) : format("test%d", idx)])
  source   = "./test"
  role     = each.key
}
