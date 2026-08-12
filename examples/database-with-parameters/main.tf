terraform {
  required_providers {
    postgresql = {
      source  = "cyrilgdn/postgresql"
    }
  }
}

provider "postgresql" {
  host            = "localhost"
  port            = 5432
  username        = "postgres"
  password        = "postgres"
  sslmode         = "disable"
  connect_timeout = 15
}

# Create the role that will own the database
resource "postgresql_role" "myapp_owner" {
  name  = "myapp_owner"
  login = true

  parameter {
    name  = "idle_session_timeout"
    value = "50000"
    quote = false
  }
}

# Create a database with configuration parameters
resource "postgresql_database" "app_db" {
  name              = "myapp_db"
  owner             = postgresql_role.myapp_owner.name
  connection_limit  = 100
  allow_connections = true

  # Set max parallel workers
  parameter {
    name  = "max_parallel_workers"
    value = "4"
    quote = false
  }

  # Set statement timeout (in milliseconds)
  parameter {
    name  = "statement_timeout"
    value = "40000"
    quote = false
  }

  # Set default statistics target
  parameter {
    name  = "default_statistics_target"
    value = "100"
    quote = false
  }
}

# Example with a quoted string parameter
resource "postgresql_database" "app_db_with_search_path" {
  name = "myapp_db2"

  # Set custom search path
  parameter {
    name  = "search_path"
    value = "public,app_schema"
  }
}

