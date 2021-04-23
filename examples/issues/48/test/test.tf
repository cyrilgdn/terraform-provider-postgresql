variable "role" {}
variable "grant_roles" {
  type = map
  default = {
    user1 = ["SELECT", "INSERT", "UPDATE", "DELETE", "TRUNCATE", "REFERENCES", "TRIGGER"],
    user2 = ["SELECT", "INSERT", "UPDATE", "DELETE", "TRUNCATE", "REFERENCES", "TRIGGER"],
    user3 = ["SELECT"]
  }
}

resource "postgresql_role" "owner" {
  name = "owner_${var.role}"
}

resource "postgresql_role" "role" {
  name = var.role
}

resource "postgresql_database" "db" {
  name  = var.role
  owner = postgresql_role.role.name
}

resource "postgresql_grant" "g" {
  for_each = var.grant_roles

  role        = postgresql_role.role.name
  database    = postgresql_database.db.name
  schema      = "public"
  object_type = "table"
  privileges  = each.value
}

resource "postgresql_default_privileges" "dp" {
  for_each = var.grant_roles

  role        = postgresql_role.role.name
  database    = postgresql_database.db.name
  owner       = postgresql_role.owner.name
  schema      = "public"
  object_type = "table"
  privileges  = each.value
}
