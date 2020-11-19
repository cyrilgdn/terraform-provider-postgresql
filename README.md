Terraform Provider
==================

- Website: https://www.terraform.io
- [![Gitter chat](https://badges.gitter.im/hashicorp-terraform/Lobby.png)](https://gitter.im/hashicorp-terraform/Lobby)
- Mailing list: [Google Groups](http://groups.google.com/group/terraform-tool)

<img src="https://cdn.rawgit.com/hashicorp/terraform-website/master/content/source/assets/images/logo-hashicorp.svg" width="600px">

Requirements
------------

-	[Terraform](https://www.terraform.io/downloads.html) 0.10.x
-	[Go](https://golang.org/doc/install) 1.11 (to build the provider plugin)

Building The Provider
---------------------

Clone repository to: `$GOPATH/src/github.com/terraform-providers/terraform-provider-postgresql`

```sh
$ mkdir -p $GOPATH/src/github.com/terraform-providers; cd $GOPATH/src/github.com/terraform-providers
$ git clone git@github.com:terraform-providers/terraform-provider-postgresql
```

Enter the provider directory and build the provider

```sh
$ cd $GOPATH/src/github.com/terraform-providers/terraform-provider-postgresql
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
- If ran locally `docker-compose` needs to be in the `$PATH`

```sh
$ make testacc
```

In order to manually run some Acceptance test locally, run the following commands:
```sh
# spins up a local docker postgres container
make testacc_setup 

# Load the needed environment variables for the tests
source tests/env.sh

# Run the test(s) that you're working on as often as you want
TF_LOG=INFO go test -v ./postgresql -run ^TestAccPostgresqlRole_Basic$

# cleans the env and tears down the postgres container
make testacc_cleanup
```
