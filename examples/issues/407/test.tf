terraform {
  required_version = ">= 1.0"

  required_providers {
    postgresql = {
      source  = "cyrilgdn/postgresql"
      version = "~>1"
    }
  }
}

provider "postgresql" {
  superuser = false
  port      = 25432
  username  = "rds"
  password  = "rds"
  sslmode   = "disable"
}

resource "postgresql_role" "test_role_with_createrole_self_grant" {
  name                  = "test_role_with_createrole_self_grant"
  parameters {
    createrole_self_grant = "set,inherit"
  }
}
