terraform {
  required_providers {
    docker = {
      source  = "kreuzwerker/docker"
      version = ">= 3.0.2"
    }
    postgresql = {
      source  = "cyrilgdn/postgresql"
      version = "1.21"
    }
    # postgresql = {
    #   source  = "terraform-fu.bar/terraform-provider-postgresql/postgresql"
    #   version = ">= 1.20"
    # }
  }
}

provider "docker" {
  host = "unix:///var/run/docker.sock"
}

resource "docker_image" "postgres" {
  name         = var.postgres_image
  keep_locally = var.keep_image
}

resource "docker_container" "postgres" {
  image = docker_image.postgres.image_id
  name  = "postgres"
  wait  = true
  ports {
    internal = var.POSTGRES_PORT
    external = var.POSTGRES_PORT
  }
  env = [
    "POSTGRES_PASSWORD=${var.POSTGRES_PASSWORD}"
  ]
  healthcheck {
    test         = ["CMD-SHELL", "pg_isready"]
    interval     = "5s"
    timeout      = "5s"
    retries      = 5
    start_period = "2s"
  }
}

provider "postgresql" {
  scheme    = "postgres"
  host      = var.POSTGRES_HOST
  port      = docker_container.postgres.ports[0].external
  database  = var.POSTGRES_PASSWORD
  username  = var.POSTGRES_PASSWORD
  password  = var.POSTGRES_PASSWORD
  sslmode   = "disable"
  superuser = false
}

resource "postgresql_database" "this" {
  name  = "test"
  owner = var.POSTGRES_USER
}

resource "postgresql_role" "readonly_role" {
  name             = "readonly"
  login            = false
  superuser        = false
  create_database  = false
  create_role      = false
  inherit          = false
  replication      = false
  connection_limit = -1
}

resource "postgresql_role" "readwrite_role" {
  name             = "readwrite"
  login            = false
  superuser        = false
  create_database  = false
  create_role      = false
  inherit          = false
  replication      = false
  connection_limit = -1
}

resource "postgresql_grant" "readonly_role" {
  database          = postgresql_database.this.name
  role              = postgresql_role.readonly_role.name
  object_type       = "table"
  schema            = "public"
  privileges        = ["SELECT"]
  with_grant_option = false
}

resource "postgresql_grant" "readwrite_role" {
  database          = postgresql_database.this.name
  role              = postgresql_role.readwrite_role.name
  object_type       = "table"
  schema            = "public"
  privileges        = ["SELECT", "INSERT", "UPDATE", "DELETE"]
  with_grant_option = false
}

resource "postgresql_role" "readonly_users" {
  for_each         = toset(local.read_only_users)
  name             = each.key
  roles            = [postgresql_role.readonly_role.name]
  login            = true
  superuser        = false
  create_database  = false
  create_role      = false
  inherit          = true
  replication      = false
  connection_limit = -1
}

resource "postgresql_role" "readwrite_users" {
  for_each         = toset(local.read_write_users)
  name             = each.key
  roles            = [postgresql_role.readonly_role.name]
  login            = true
  superuser        = false
  create_database  = false
  create_role      = false
  inherit          = true
  replication      = false
  connection_limit = -1
}

resource "postgresql_grant" "connect_db_readonly_role" {
  database    = postgresql_database.this.name
  object_type = "database"
  privileges  = ["CREATE", "CONNECT"]
  role        = postgresql_role.readonly_role.name
}

resource "postgresql_grant" "connect_db_readwrite_role" {
  database    = postgresql_database.this.name
  object_type = "database"
  privileges  = ["CREATE", "CONNECT"]
  role        = postgresql_role.readwrite_role.name
}

resource "postgresql_grant" "usage_readonly_role" {
  database          = postgresql_database.this.name
  role              = postgresql_role.readonly_role.name
  object_type       = "schema"
  schema            = "public"
  privileges        = ["USAGE"]
  with_grant_option = false
}

resource "postgresql_grant" "usage_readwrite_role" {
  database          = postgresql_database.this.name
  role              = postgresql_role.readwrite_role.name
  object_type       = "schema"
  schema            = "public"
  privileges        = ["USAGE"]
  with_grant_option = false
}

