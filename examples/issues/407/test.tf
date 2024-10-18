resource "postgresql_role" "this" {
  name     = "test"
  login    = true
  password = "test"
}

resource "postgresql_database" "this" {
  name              = "test"
  owner             = postgresql_role.this.name
  lc_collate        = "en_US.utf8"
  allow_connections = true
}
