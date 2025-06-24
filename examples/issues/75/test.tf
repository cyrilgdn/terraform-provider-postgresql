terraform {
  required_providers {
    postgresql = {
      source  = "rlmartin/postgresql"
      version = ">=1.12"
    }
  }
}

data "terraform_remote_state" "rds" {
  backend = "local"

  config = {
    path = "../../rds/terraform.tfstate"
  }
}

provider "postgresql" {
  host      = data.terraform_remote_state.rds.outputs.db.address
  port      = data.terraform_remote_state.rds.outputs.db.port
  database  = "postgres"
  username  = data.terraform_remote_state.rds.outputs.db.username
  password  = data.terraform_remote_state.rds.outputs.db.password
  sslmode   = "require"
  superuser = false
}

resource "postgresql_database" "test" {
  name = "test"
}

resource "postgresql_role" "test" {
  name = "test"
}

resource "postgresql_role" "test_readonly" {
  name     = "test_readonly"
  login    = true
  password = "toto"
}

resource "postgresql_grant" "grant_ro_sequence" {
  database    = postgresql_database.test.name
  role        = postgresql_role.test_readonly.name
  schema      = "public"
  object_type = "sequence"
  privileges  = ["USAGE", "SELECT"]
}

resource "postgresql_grant" "grant_ro_tables" {
  database    = postgresql_database.test.name
  role        = postgresql_role.test_readonly.name
  schema      = "public"
  object_type = "table"
  privileges  = ["SELECT"]
}

resource "postgresql_default_privileges" "alter_ro_tables" {
  database    = postgresql_database.test.name
  owner       = postgresql_role.test.name
  role        = postgresql_role.test_readonly.name
  schema      = "public"
  object_type = "table"
  privileges  = ["SELECT"]
}

resource "postgresql_default_privileges" "alter_ro_sequence" {
  database    = postgresql_database.test.name
  owner       = postgresql_role.test.name
  role        = postgresql_role.test_readonly.name
  schema      = "public"
  object_type = "sequence"
  privileges  = ["USAGE", "SELECT"]
}

resource "postgresql_grant" "revoke_public" {
  database    = postgresql_database.test.name
  role        = "public"
  schema      = "public"
  object_type = "schema"
  privileges  = []

  with_grant_option = true
}
