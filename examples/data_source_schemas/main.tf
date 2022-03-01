terraform {
    required_version = ">= 0.12"
      required_providers {
    postgresql = {
      source  = "cyrilgdn/postgresql"
      version = ">=1.12"
    }
  }
}

provider "postgresql" {
    host = "127.0.0.1"
    port = 5432
    database = "iris-ar"
    username = "Spencer.Xia"
    sslmode = "disable"
    connect_timeout = 15
}

data "postgresql_schemas" "retrieve_schemas_test" {
    database = "iris-ar"
}

data "postgresql_schemas" "retrieve_schemas_test_with_system" {
    database = "iris-ar"
    include_system_schemas = true
}

data "postgresql_schemas" "retrieve_schemas_test_iris-uls" {
    database = "iris-uls"
}

data "postgresql_schemas" "retrieve_schemas_test_and_patterns" {
    database = "iris-ar"
    include_system_schemas = false
    not_like_pattern = "%*%"
}

data "postgresql_schemas" "retrieve_schemas_test_with_system_and_patterns" {
    database = "iris-ar"
    include_system_schemas = true
    like_pattern = "pg_%"
    regex_pattern = "^pg_toast.*$"
}

output "iris-ar_schemas" {
    value = data.postgresql_schemas.retrieve_schemas_test.schemas
}

output "iris-ar-schemas_inc_system" {
    value = data.postgresql_schemas.retrieve_schemas_test_with_system.schemas
}

output "iris-uls_schemas" {
    value = data.postgresql_schemas.retrieve_schemas_test_iris-uls.schemas
}

output "iris-ar_schemas_with_patterns" {
    value = data.postgresql_schemas.retrieve_schemas_test_and_patterns.schemas
}

output "iris-ar_schemas_inc_system_with_patterns" {
    value = data.postgresql_schemas.retrieve_schemas_test_with_system_and_patterns.schemas
}