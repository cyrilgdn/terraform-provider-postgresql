Terraform Provider for PostgreSQL
=================================

This provider allows to manage with Terraform [Postgresql](https://www.postgresql.org/) objects like databases, extensions, roles, etc.

It's published on the [Terraform registry](https://registry.terraform.io/providers/cyrilgdn/postgresql/latest/docs).
It replaces https://github.com/hashicorp/terraform-provider-postgresql since Hashicorp stopped hosting community providers in favor of the Terraform registry.

- Documentation: https://registry.terraform.io/providers/cyrilgdn/postgresql/latest/docs

Requirements
------------

-	[Terraform](https://www.terraform.io/downloads.html) 0.12.x
-	[Go](https://golang.org/doc/install) 1.16 (to build the provider plugin)

Building The Provider
---------------------

Clone repository to: `$GOPATH/src/github.com/cyrilgdn/terraform-provider-postgresql`

```sh
$ mkdir -p $GOPATH/src/github.com/cyrilgdn; cd $GOPATH/src/github.com/cyrilgdn
$ git clone git@github.com:cyrilgdn/terraform-provider-postgresql
```

Enter the provider directory and build the provider

```sh
$ cd $GOPATH/src/github.com/cyrilgdn/terraform-provider-postgresql
$ make build
```

Using the provider
----------------------

Usage examples can be found in the Terraform [provider documentation](https://www.terraform.io/docs/providers/postgresql/index.html)

Developing the Provider
---------------------------

If you wish to work on the provider, you'll first need [Go](http://www.golang.org) installed on your machine (version 1.11+ is *required*). You'll also need to correctly setup a [GOPATH](http://golang.org/doc/code.html#GOPATH), as well as adding `$GOPATH/bin` to your `$PATH`.

To compile the provider, run `make build`. This will build the provider and put the provider binary in the `$GOPATH/bin` directory.

```sh
$ make build
...
$ $GOPATH/bin/terraform-provider-postgresql
...
```

In order to test the provider, you can simply run `make test`.

```sh
$ make test
```

In order to run the full suite of Acceptance tests, run `make testacc`.

*Note:* 
- Acceptance tests create real resources, and often cost money to run.

```sh
$ make testacc
```

In order to manually run some Acceptance test locally, run the following commands:
```sh
# spins up a local docker postgres container
make testacc_setup 

# Load the needed environment variables for the tests
source tests/switch_superuser.sh

# Run the test(s) that you're working on as often as you want
TF_LOG=INFO go test -v ./postgresql -run ^TestAccPostgresqlRole_Basic$

# cleans the env and tears down the postgres container
make testacc_cleanup 
```
