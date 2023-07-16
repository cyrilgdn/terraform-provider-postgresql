---
layout: "postgresql"
page_title: "Provider: PostgreSQL"
sidebar_current: "docs-postgresql-index"
description: |-
  A provider for PostgreSQL Server.
---

# PostgreSQL Provider

The PostgreSQL provider gives the ability to deploy and configure resources in a PostgreSQL server.

Use the navigation to the left to read about the available resources.

## Usage

```hcl
provider "postgresql" {
  host            = "postgres_server_ip"
  port            = 5432
  database        = "postgres"
  username        = "postgres_user"
  password        = "postgres_password"
  sslmode         = "require"
  connect_timeout = 15
}
```

An SSL client certificate can be configured using the `clientcert` sub-resource.

``` hcl
provider "postgresql" {
  host            = "postgres_server_ip"
  port            = 5432
  database        = "postgres"
  username        = "postgres_user"
  password        = "postgres_password"
  sslmode         = "require"
  clientcert {
    cert = "/path/to/public-certificate.pem"
    key  = "/path/to/private-key.pem"
  }
}
```

Configuring multiple servers can be done by specifying the alias option.

```hcl
provider "postgresql" {
  alias    = "pg1"
  host     = "postgres_server_ip1"
  username = "postgres_user1"
  password = "postgres_password1"
}

provider "postgresql" {
  alias    = "pg2"
  host     = "postgres_server_ip2"
  username = "postgres_user2"
  password = "postgres_password2"
}

resource "postgresql_database" "my_db1" {
  provider = "postgresql.pg1"
  name     = "my_db1"
}

resource "postgresql_database" "my_db2" {
  provider = "postgresql.pg2"
  name     = "my_db2"
}
```

## Argument Reference

The following arguments are supported:

* `scheme` - (Optional) The driver to use. Valid values are:
  * `postgres`: Default value, use [`lib/pq`][libpq]
  * `awspostgres`: Use [GoCloud](#gocloud) for AWS
  * `gcppostgres`: Use [GoCloud](#gocloud) for GCP
* `host` - (Required) The address for the postgresql server connection, see [GoCloud](#gocloud) for specific format.
* `port` - (Optional) The port for the postgresql server connection. The default is `5432`.
* `database` - (Optional) Database to connect to. The default is `postgres`.
* `username` - (Required) Username for the server connection.
* `password` - (Optional) Password for the server connection.
* `database_username` - (Optional) Username of the user in the database if different than connection username (See [user name maps](https://www.postgresql.org/docs/current/auth-username-maps.html)).
* `superuser` - (Optional) Should be set to `false` if the user to connect is not a PostgreSQL superuser (as is the case in AWS RDS or GCP SQL).
*                          In this case, some features might be disabled (e.g.: Refreshing state password from database).
* `sslmode` - (Optional) Set the priority for an SSL connection to the server.
  Valid values for `sslmode` are (note: `prefer` is not supported by Go's
  [`lib/pq`][libpq])):
    * disable - No SSL
    * require - Always SSL (the default, also skip verification)
    * verify-ca - Always SSL (verify that the certificate presented by the server was signed by a trusted CA)
    * verify-full - Always SSL (verify that the certification presented by the server was signed by a trusted CA and the server host name matches the one in the certificate)
  Additional information on the options and their implications can be seen
  [in the `libpq(3)` SSL guide](http://www.postgresql.org/docs/current/static/libpq-ssl.html#LIBPQ-SSL-PROTECTION).
* `clientcert` - (Optional) - Configure the SSL client certificate.
  * `cert` - (Required) - The SSL client certificate file path. The file must contain PEM encoded data.
  * `key` - (Required) - The SSL client certificate private key file path. The file must contain PEM encoded data.
* `sslrootcert` - (Optional) - The SSL server root certificate file path. The file must contain PEM encoded data.
* `connect_timeout` - (Optional) Maximum wait for connection, in seconds. The
  default is `180s`.  Zero or not specified means wait indefinitely.
* `max_connections` - (Optional) Set the maximum number of open connections to
  the database. The default is `20`.  Zero means unlimited open connections.
* `expected_version` - (Optional) Specify a hint to Terraform regarding the
  expected version that the provider will be talking with.  This is a required
  hint in order for Terraform to talk with an ancient version of PostgreSQL.
  This parameter is expected to be a [PostgreSQL
  Version](https://www.postgresql.org/support/versioning/) or `current`.  Once a
  connection has been established, Terraform will fingerprint the actual
  version.  Default: `9.0.0`.
* `aws_rds_iam_auth` - (Optional) If set to `true`, call the AWS RDS API to grab a temporary password, using AWS Credentials
  from the environment (or the given profile, see `aws_rds_iam_profile`)
* `aws_rds_iam_profile` - (Optional) The AWS IAM Profile to use while using AWS RDS IAM Auth.
* `aws_rds_iam_region` - (Optional) The AWS region to use while using AWS RDS IAM Auth.

## GoCloud

By default, the provider uses the [lib/pq][libpq] library to directly connect to PostgreSQL host instance. For connections to AWS/GCP hosted instances, the provider can connect through the [GoCloud](https://gocloud.dev/howto/sql/) library. GoCloud simplifies connecting to AWS/GCP hosted databases, managing any proxy or custom authentication details.

### AWS

To enable GoCloud based connections to AWS RDS instances, set `scheme` to `awspostgres` and `host` to the RDS database's endpoint value.
(e.g.: `instance.xxxxxx.region.rds.amazonaws.com`)

```hcl
provider "postgresql" {
  scheme   = "awspostgres"
  host     = "test-instance.cvvrsv6scpgd.eu-central-1.rds.amazonaws.com"
  username = "postgres"
  port     = 5432
  password = "test1234"

  superuser = false
}
```

### GCP

To enable GoCloud for GCP SQL, set `scheme` to `gcppostgres` and `host` to the connection name of the instance in following format: `project/region/instance` (or `project:region:instance`).

For GCP, GoCloud also requires the `GOOGLE_APPLICATION_CREDENTIALS` environment variable to be set to the service account credentials file.
These credentials can be created here: https://console.cloud.google.com/iam-admin/serviceaccounts

See also: https://cloud.google.com/docs/authentication/production

---
**Note**

[Cloud SQL API](https://console.developers.google.com/apis/api/sqladmin.googleapis.com/overview) needs to be enabled for GoCloud to connect to your instance.

---

```hcl
provider "postgresql" {
  scheme   = "gcppostgres"
  host     = "test-project/europe-west3/test-instance"
  username = "postgres"
  port     = 5432
  password = "test1234"

  superuser = false
}
```

Example with GCP resources:

```hcl
resource "google_sql_database_instance" "test" {
  project          = "test-project"
  name             = "test-instance"
  database_version = "POSTGRES_13"
  region           = "europe-west3"

  settings {
    tier            = "db-f1-micro"
  }
}

resource "google_sql_user" "postgres" {
  project  = "test-project"
  name     = "postgres"
  instance = google_sql_database_instance.test.name
  password = "xxxxxxxx"
}


provider "postgresql" {
  scheme   = "gcppostgres"
  host     = google_sql_database_instance.test.connection_name
  username = google_sql_user.postgres.name
  password = google_sql_user.postgres.password
}

resource postgresql_database "test_db" {
  name = "test_db"
}
```

### SOCKS5 Proxy Support

The provider supports connecting via a SOCKS5 proxy, but when the `postgres` scheme is used. It can be configured by setting the `ALL_PROXY` or `all_proxy` environment variable to a value like `socks5://127.0.0.1:1080`.

The `NO_PROXY` or `no_proxy` environment can also be set to opt out of proxying for specific hostnames or ports.

[libpq]: https://pkg.go.dev/github.com/lib/pq
