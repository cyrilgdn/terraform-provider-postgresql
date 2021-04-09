terraform {
  required_providers {
    aws = {
      source = "hashicorp/aws"
    }
    postgresql = {
      source  = "cyrilgdn/postgresql"
      version = "1.12.0"
    }
  }
  required_version = ">= 0.14.0"
}

module "db_instance" {
  source = "./postgresql"
}

provider "postgresql" {
  host      = module.db_instance.db.address
  port      = module.db_instance.db.port
  database  = "postgres"
  username  = module.db_instance.db.username
  password  = module.db_instance.db.password
  sslmode   = "require"
  superuser = false
}

resource "postgresql_role" "test_role" {
  name     = "test_role"
  login    = true
  password = "test1234"
}

output "db_address" {
  value = module.db_instance.db.address
}
