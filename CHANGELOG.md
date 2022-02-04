## Unreleased

## 1.15.0 (February 4,  2022)

FEATURES:

* `postgresql_default_privileges`: Support default privileges for schema - @kostiantyn-nemchenko
  [#126](https://github.com/cyrilgdn/terraform-provider-postgresql/pull/126)

* `provider`: Add support for RDS IAM credentials - @Jell
  [#134](https://github.com/cyrilgdn/terraform-provider-postgresql/pull/134)

* `postgresql_grant`: Support for procedures and routines - @alec-rabold
  [#169](https://github.com/cyrilgdn/terraform-provider-postgresql/pull/169)

FIXES:

* `postgresql_grant`: fix `tuple concurrently updated` error - @boekkooi-lengoo
  [#169](https://github.com/cyrilgdn/terraform-provider-postgresql/pull/169)

DEV IMPROVEMENTS:

* Upgrade Terraform SDK to v2- @cyrilgdn
  [#140](https://github.com/cyrilgdn/terraform-provider-postgresql/pull/140)

* Remove vendor directory - @cyrilgdn (and lint fixed by @Jell )
  [#139](https://github.com/cyrilgdn/terraform-provider-postgresql/pull/139)
  [#146](https://github.com/cyrilgdn/terraform-provider-postgresql/pull/146)

* docker-compose: Use Docker healthcheck to wait for Postgres to be available - @boekkooi-lengoo
  [#168](https://github.com/cyrilgdn/terraform-provider-postgresql/pull/168)


## 1.14.0 (August 22, 2021)

FEATURES / FIXES:

* `postgresql_replication_slot`: Create resource to manage replication slots - @BarnabyShearer
  [#70](https://github.com/cyrilgdn/terraform-provider-postgresql/pull/70)

* `postgresql_physical_replication_slot`: Create resource to manage physical replication slots - @nerzhul
  [#107](https://github.com/cyrilgdn/terraform-provider-postgresql/pull/107)

* `postgresql_grant`: Add `objects` setting to manage individual objects - @alec-rabold
  [#105](https://github.com/cyrilgdn/terraform-provider-postgresql/pull/105)

* Disable `statement_timeout` for connections that need locks - @cyrilgdn
  [#123](https://github.com/cyrilgdn/terraform-provider-postgresql/pull/123)

* `postgresql_default_privileges`: Allow empty privileges list - @alec-rabold
  [#118](https://github.com/cyrilgdn/terraform-provider-postgresql/pull/118)

* `postgresql_extension`: Support CREATE EXTENSION ... CASCADE - @kostiantyn-nemchenko
  [#108](https://github.com/cyrilgdn/terraform-provider-postgresql/pull/108)

* `postgresql_grant`: add foreign data wrapper and server support - @kostiantyn-nemchenko
  [#109](https://github.com/cyrilgdn/terraform-provider-postgresql/pull/109)

DEV IMPROVEMENTS:

* Run golangci-lint in GH actions and fix errors - @cyrilgdn
  [#122](https://github.com/cyrilgdn/terraform-provider-postgresql/pull/122)

DOCUMENTATION:

* `postgresql_grant`: Add missing `with_grant_option` documentation - @ryancausey
  [#64](https://github.com/cyrilgdn/terraform-provider-postgresql/pull/64)

## 1.13.0 (May 21, 2021)

FEATURES / FIXES:

* Stop locking catalog for every resources - @cyrilgdn
  [#80](https://github.com/cyrilgdn/terraform-provider-postgresql/pull/80)

DOCUMENTATION:

* Add miss `drop_cascade` docs for `postgresql_extension` - @MaxymVlasov
  [#89](https://github.com/cyrilgdn/terraform-provider-postgresql/pull/89)

## 1.12.1 (April 23, 2021)

FEATURES:

* Update Go version to 1.16: This allows builds for for darwin arm64 - @benfdking
  [#76](https://github.com/cyrilgdn/terraform-provider-postgresql/pull/76)

## 1.12.0 (March 26, 2021)

FEATURES:

* `postgresql_default_privileges`: Add `with_grant_option` - @stawii, @cyrilgdn
  [#10](https://github.com/cyrilgdn/terraform-provider-postgresql/pull/10) /
  [#63](https://github.com/cyrilgdn/terraform-provider-postgresql/pull/63)

* `postgresql_default_privileges`: Make `schema` optional for default privileges on all schemas - @darren-reddick
  [#59](https://github.com/cyrilgdn/terraform-provider-postgresql/pull/59)

* `postgresql_role`: Add `idle_in_transaction_session_timeout` suppor - @colesnodgrass
  [#39](https://github.com/cyrilgdn/terraform-provider-postgresql/pull/39)

## 1.11.2 (February 16, 2021)

FIXES:

* Fix connect_timeout for gocloud - @ynaka81
  [#56](https://github.com/cyrilgdn/terraform-provider-postgresql/pull/56)


## 1.11.1 (February 2, 2021)

FIXES:

* Fix building of connection string parameters - @mcwarman
  [#47](https://github.com/cyrilgdn/terraform-provider-postgresql/pull/47)


## 1.11.0 (January 10, 2021)

FEATURES:

* gocloud: Allow to connect with [GoCloud](https://gocloud.dev/howto/sql/) to AWS/GCP instances - @cyrilgdn
  [#29](https://github.com/cyrilgdn/terraform-provider-postgresql/pull/29)

* `postgresql_grant`: Manage grant on schema - @cyrilgdn
  [#30](https://github.com/cyrilgdn/terraform-provider-postgresql/pull/30)

  :warning: This depreciates the `policy` attribute in `postgresql_schema`

* `postgresql_grant`: Allow an empty privileges list (revoke) - @cyrilgdn
  [#26](https://github.com/cyrilgdn/terraform-provider-postgresql/pull/26)

* `postgresql_grant` / `postgresql_default_privileges`: Manage `PUBLIC` role - @cyrilgdn
  [#27](https://github.com/cyrilgdn/terraform-provider-postgresql/pull/27)

## 1.10.0 (January 2, 2021)

FEATURES:

* `postgresql_database`: Drop connections before drop database (Postgresql >=13) - @p4cket
  [#14](https://github.com/cyrilgdn/terraform-provider-postgresql/pull/14)

  :warning: In previous versions, Terraform failed to drop databases if they are still in used.
            Now databases will be dropped in any case.

* Use lazy connections - @cyrilgdn
  [#5](https://github.com/cyrilgdn/terraform-provider-postgresql/pull/5)


## 1.9.0 (December 21, 2020)

FEATURES:
* `postgresql_grant_role` (New resource): Grant role to another role - @dvdliao
  [#4](https://github.com/cyrilgdn/terraform-provider-postgresql/pull/4)

FIXES:

* `postgresql_role`: Fix quoted search_path - @lovromazgon
  [#1](https://github.com/cyrilgdn/terraform-provider-postgresql/pull/1)

* `postgresql_grant`: Fix SQL error on function - @p4cket
  [#15](https://github.com/cyrilgdn/terraform-provider-postgresql/pull/15)

## 1.8.1 (November 26, 2020)

BUG FIXES:

* Revert "Use lazy connections" [#199](https://github.com/terraform-providers/terraform-provider-postgresql/pull/199)
  Plugin panics if not able to connect to the database.

## 1.8.0 (November 26, 2020)

FEATURES:

* `postgresql_extension`: Support drop cascade.
  ([#162](https://github.com/terraform-providers/terraform-provider-postgresql/pull/162) - @multani)

* ~~Use lazy connections.
  ([#199](https://github.com/terraform-providers/terraform-provider-postgresql/pull/199) - @estahn)~~ (Reverted in 1.8.1)

BUG FIXES:

* `postgresql_grant`: Fix grant on function by removing `prokind` column selection.
  ([#171](https://github.com/terraform-providers/terraform-provider-postgresql/pull/171) - @Tommi2Day)

DEV IMPROVEMENTS:

* Set up Github Workflows to create releases.
  ([#3](https://github.com/cyrilgdn/terraform-provider-postgresql/pull/3) - @thenonameguy)

## 1.7.2 (July 30, 2020)

This is the first release on [Terraform registry](https://registry.terraform.io/providers/cyrilgdn/postgresql/latest)

DEV IMPROVEMENTS:

* Add goreleaser config
* Pusblish on Terraform registry: https://registry.terraform.io/providers/cyrilgdn/postgresql/latest

## 1.7.1 (July 30, 2020)

BUG FIXES:

* all resources: Fix some specific use case on `withRolesGranted` helper.
  ([#162](https://github.com/terraform-providers/terraform-provider-postgresql/pull/162))

* `postgresql_role`: Fix `bypass_row_level_security` attribute.
  ([#158](https://github.com/terraform-providers/terraform-provider-postgresql/pull/158))

## 1.7.0 (July 17, 2020)

FEATURES:

* all resources: Grant object owners to connected user when needed.
  This greatly improves support of non-superuser admin (e.g.: on AWS RDS)
  ([#146](https://github.com/terraform-providers/terraform-provider-postgresql/pull/146))

* `postgresql_grant`, `postgresql_default_privileges`: Implement grant on functions.
  ([#144](https://github.com/terraform-providers/terraform-provider-postgresql/pull/144))

* `postgresql_default_privileges`: Implement grant on type.
  ([#134](https://github.com/terraform-providers/terraform-provider-postgresql/pull/134))

DEV IMPROVEMENTS:

* Upgrade to Go 1.14 and replace errwrap.Wrapf by fmt.Errorf.
  ([#145](https://github.com/terraform-providers/terraform-provider-postgresql/pull/145))


DOCUMENTATION:

* Improve documentation of `postgresql_grant`
  ([#149](https://github.com/terraform-providers/terraform-provider-postgresql/pull/149) and [#151](https://github.com/terraform-providers/terraform-provider-postgresql/pull/151))

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
