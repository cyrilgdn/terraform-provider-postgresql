### 0.3.0 (Unreleased)

FEATURES:

* New resource: postgresql_grant. This resource allows to grant privileges on all existing tables or sequences for a specified role in a specified schema.
  ([#53](https://github.com/terraform-providers/terraform-provider-postgresql/pull/53))

## 0.2.1 (February 28, 2019)

BUG FIXES:

* `provider`: Add a `superuser` setting to fix role password update when provider is not connected as a superuser.
  ([#66](https://github.com/terraform-providers/terraform-provider-postgresql/pull/66))

## 0.2.0 (February 21, 2019)
FEATURES:

* Add `database_username` in provider configuration to manage [user name maps](https://www.postgresql.org/docs/current/auth-username-maps.html) (e.g.: needed for Azure)
  ([#58](https://github.com/terraform-providers/terraform-provider-postgresql/pull/58))

BUG FIXES:

* `create_database` is now being applied correctly on role creation
  ([#43](https://github.com/terraform-providers/terraform-provider-postgresql/pull/43))
* Updating a role password doesn't actually update the role password
  ([#54](https://github.com/terraform-providers/terraform-provider-postgresql/pull/54))
* `superuser` is now being applied correctly on role creation
  ([#45](https://github.com/terraform-providers/terraform-provider-postgresql/pull/45))
* Feature flag system was not working.
  ([#61](https://github.com/terraform-providers/terraform-provider-postgresql/pull/61))
* Updating database does not work for connection_limit / allow_connection / is_template
  ([#61](https://github.com/terraform-providers/terraform-provider-postgresql/pull/61))
* Disable postgresql_extension for Postgres < 9.1
  ([#61](https://github.com/terraform-providers/terraform-provider-postgresql/pull/61))
* Disable REPLICATION flag in role for Postgres < 9.1.
  ([#61](https://github.com/terraform-providers/terraform-provider-postgresql/pull/61))
* `postgresql_database`: Fix the way the database owner is granted / revoked during create/update/delete.
  ([#59](https://github.com/terraform-providers/terraform-provider-postgresql/pull/59))

TESTS:

* Travis: Run acceptance tests against multiple Postgres versions.

## 0.1.3 (December 19, 2018)

BUG FIXES:

* Parse Azure PostgreSQL version
  ([#40](https://github.com/terraform-providers/terraform-provider-postgresql/pull/40))

## 0.1.2 (July 06, 2018)

FEATURES:

* support for Postgresql v10 ([#31](https://github.com/terraform-providers/terraform-provider-postgresql/issues/31))

## 0.1.1 (January 19, 2018)

DEPRECATED:

* `provider`: `sslmode` is the correct spelling for the various SSL modes.  Mark
  `ssl_mode` as the deprecated spelling.  In probably 6mo time `ssl_mode` will
  be removed as an alternate spelling.
  [https://github.com/terraform-providers/terraform-provider-postgresql/pull/27]

BUG FIXES:

* Mark Provider `password` as sensitive.
  [https://github.com/terraform-providers/terraform-provider-postgresql/pull/26]
* Fix destruction of databases created in RDS.
  [https://github.com/terraform-providers/terraform-provider-postgresql/issues/17]
* Fix DEFAULT values for the `postgresql_database` resource.
  [https://github.com/terraform-providers/terraform-provider-postgresql/issues/9]

## 0.1.0 (June 21, 2017)

NOTES:

* Same functionality as that of Terraform 0.9.8. Repacked as part of [Provider Splitout](https://www.hashicorp.com/blog/upcoming-provider-changes-in-terraform-0-10/)
