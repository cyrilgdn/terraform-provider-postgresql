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

data "postgresql_schemas_data_source" "retrieve_schemas_test" {
    database = "iris-ar"
}


data "postgresql_schemas_data_source" "retrieve_schemas_test_with_system" {
    database = "iris-ar"
    include_system_schemas = true
}

output "iris-ar_schemas" {
    value = data.postgresql_schemas_data_source.retrieve_schemas_test.schemas
}

output "iris-ar-schemas_inc_system" {
    value = data.postgresql_schemas_data_source.retrieve_schemas_test_with_system.schemas
}