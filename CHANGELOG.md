## 0.1.4 (Unreleased)
BUG FIXES:

* `create_database` is now being applied correctly on role creation
  ([#43](https://github.com/terraform-providers/terraform-provider-postgresql/pull/43))
* Updating a role password doesn't actually update the role password
  [https://github.com/terraform-providers/terraform-provider-postgresql/issues/16]
  ([#54](https://github.com/terraform-providers/terraform-provider-postgresql/pull/43))
* `superuser` is now being applied correctly on role creation
  ([#45](https://github.com/terraform-providers/terraform-provider-postgresql/pull/43))

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
