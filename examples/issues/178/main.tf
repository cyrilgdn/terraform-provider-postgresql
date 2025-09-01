terraform {
  required_providers {
    docker = {
      source  = "kreuzwerker/docker"
      version = ">= 3.0.2"
    }
    postgresql = {
      source  = "cyrilgdn/postgresql"
      version = ">= 1.25"
    }
  }
}

provider "docker" {
  host = var.docker_host
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
  upload {
    file    = "/docker-entrypoint-initdb.d/mock-tables.sql"
    content = <<EOS
      CREATE DATABASE "test" OWNER "${var.POSTGRES_DBNAME}";
      \connect ${var.POSTGRES_DBNAME}

      DO $$
      DECLARE
        table_count int := ${var.table_count};
      BEGIN
        FOR count IN 0..table_count LOOP
          EXECUTE format('CREATE TABLE table_%s (test int)', count);
        END LOOP;
      END $$;
    EOS
  }
}

provider "postgresql" {
  scheme      = "postgres"
  host        = var.POSTGRES_HOST
  port        = docker_container.postgres.ports[0].external
  database    = var.POSTGRES_PASSWORD
  username    = var.POSTGRES_PASSWORD
  password    = var.POSTGRES_PASSWORD
  sslmode     = "disable"
  superuser   = false
  lock_grants = true
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
  database          = var.POSTGRES_DBNAME
  role              = postgresql_role.readonly_role.name
  object_type       = "table"
  schema            = "public"
  privileges        = ["SELECT"]
  with_grant_option = false
}

resource "postgresql_grant" "readwrite_role" {
  database          = var.POSTGRES_DBNAME
  role              = postgresql_role.readwrite_role.name
  object_type       = "table"
  schema            = "public"
  privileges        = ["SELECT", "INSERT", "UPDATE", "DELETE"]
  with_grant_option = false
}

resource "postgresql_role" "readonly_users" {
  for_each         = local.read_only_users
  name             = each.value
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
  for_each         = local.read_write_users
  name             = each.value
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
  for_each    = postgresql_role.readonly_users
  database    = var.POSTGRES_DBNAME
  object_type = "database"
  privileges  = ["CREATE", "CONNECT"]
  role        = each.value.name
}

resource "postgresql_grant" "connect_db_readwrite_role" {
  for_each    = postgresql_role.readwrite_users
  database    = var.POSTGRES_DBNAME
  object_type = "database"
  privileges  = ["CREATE", "CONNECT"]
  role        = each.value.name
}

resource "postgresql_grant" "usage_readonly_role" {
  for_each          = postgresql_role.readonly_users
  database          = var.POSTGRES_DBNAME
  role              = each.value.name
  object_type       = "schema"
  schema            = "public"
  privileges        = ["USAGE"]
  with_grant_option = false
}

resource "postgresql_grant" "usage_readwrite_role" {
  for_each          = postgresql_role.readwrite_users
  database          = var.POSTGRES_DBNAME
  role              = each.value.name
  object_type       = "schema"
  schema            = "public"
  privileges        = ["USAGE"]
  with_grant_option = false
}

resource "postgresql_grant" "select_readonly_role" {
  for_each          = postgresql_role.readonly_users
  database          = var.POSTGRES_DBNAME
  role              = each.value.name
  object_type       = "table"
  schema            = "public"
  privileges        = ["SELECT"]
  with_grant_option = false
}

resource "postgresql_grant" "crud_readwrite_role" {
  for_each          = postgresql_role.readwrite_users
  database          = var.POSTGRES_DBNAME
  role              = each.value.name
  object_type       = "table"
  schema            = "public"
  privileges        = ["SELECT", "UPDATE", "INSERT", "DELETE"]
  with_grant_option = false
}
