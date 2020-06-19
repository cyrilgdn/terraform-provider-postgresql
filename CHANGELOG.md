## 1.7.0 (Unreleased)

FEATURES:

* `postgresql_grant`: Implement grant on functions

## 1.6.0 (May 22, 2020)

FEATURES:

* `postgresql_grant`: Implement grant on database.
  ([#123](https://github.com/terraform-providers/terraform-provider-postgresql/pull/123))

* Support client/server SSL certificates.
  ([#126](https://github.com/terraform-providers/terraform-provider-postgresql/pull/126))

* Use SDK validations functions instead of custom ones.
  ([#122](https://github.com/terraform-providers/terraform-provider-postgresql/pull/122))

BUG FIXES:

* Fix `max_connections` validation to allow 0 (unlimited).
  ([#128](https://github.com/terraform-providers/terraform-provider-postgresql/pull/128))


## 1.5.0 (February 23, 2020)

FEATURES:

* `postgresql_role`: Allow to configure `statement_timeout` for a role.
  ([#105](https://github.com/terraform-providers/terraform-provider-postgresql/pull/105))

FIXES:

* Don't md5 SCRAM-SHA-256 passwords.
  ([#114](https://github.com/terraform-providers/terraform-provider-postgresql/pull/114))

DEV IMPROVEMENTS:

* Upgrade lib/pq to v1.3.0 to support SCRAM-SHA-256 password.
  ([#113](https://github.com/terraform-providers/terraform-provider-postgresql/pull/113))

DOCUMENTATION:

* Update the "use" section to link to the official provider usage documentation.
  ([#115](https://github.com/terraform-providers/terraform-provider-postgresql/pull/115))

* `postgresql_schema`: Add missing documentation for `database` setting.
  ([#118](https://github.com/terraform-providers/terraform-provider-postgresql/pull/118))

## 1.4.0 (December 25, 2019)

FEATURES:

* `postgresql_schema`: Add `database` attribute.
  ([#100](https://github.com/terraform-providers/terraform-provider-postgresql/pull/100))

* `provider`: Trust expected_version if provided
  ([#103](https://github.com/terraform-providers/terraform-provider-postgresql/pull/103))
  This allows to disable the version detection which requires a database connection, so plan on empty state does not require a connection.

* `postgresql_schema`: Add `drop_cascade` attribute.
  ([#108](https://github.com/terraform-providers/terraform-provider-postgresql/pull/108))


## 1.3.0 (November 01, 2019)

FEATURES:

* `postgresql_role`: Add `search_path` attribute.
  ([#93](https://github.com/terraform-providers/terraform-provider-postgresql/pull/93))

## 1.2.0 (September 26, 2019)

BUG FIXES:

* `postgresql_default_privileges`: Grant owner to connected role before applying default privileges.
  ([#71](https://github.com/terraform-providers/terraform-provider-postgresql/pull/71))

NOTES:

* Terraform SDK migrated to new standalone Terraform plugin SDK v1.0.0


## 1.1.0 (July 04, 2019)

FEATURES:

* `postgresql_extension`: allow to create extension on another database from the provider.


## 1.0.0 (June 21, 2019)

BREAKING CHANGES:

* `postgresql_role`: Remove default value for password field.

FEATURES:

* Terraform v0.12 compatibility: Terraform SDK has been upgraded to v0.12.2.


## 0.4.0 (May 15, 2019)

FEATURES:

* `postgresql_role`: Add `roles` attribute.
  ([#52](https://github.com/terraform-providers/terraform-provider-postgresql/pull/52))

BUG FIXES:

* `postgresql_grant`, `postgresql_default_privileges`: Fix schema verification.
  ([#74](https://github.com/terraform-providers/terraform-provider-postgresql/pull/74))

* `postgresql_extension`: Add `IF NOT EXISTS` when creating extension.
  ([#76](https://github.com/terraform-providers/terraform-provider-postgresql/pull/76))

### 0.3.0 (April 16, 2019)

FEATURES:

* New resource: postgresql_grant. This resource allows to grant privileges on all existing tables or sequences for a specified role in a specified schema.
  ([#53](https://github.com/terraform-providers/terraform-provider-postgresql/pull/53))
* New resource: postgresql_default_privileges. This resource allow to manage default privileges for tables or sequences for a specified role in a specified schema.
  ([#53](https://github.com/terraform-providers/terraform-provider-postgresql/pull/53))

BUG FIXES:

* `postgresql_role`: Fix syntax error with `valid_until` attribute.
  ([#69](https://github.com/terraform-providers/terraform-provider-postgresql/pull/69))

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
