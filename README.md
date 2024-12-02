Terraform Provider for PostgreSQL with CockroachDB compatibility
=================================

This provider allows to manage with Terraform [Postgresql](https://www.postgresql.org/) objects like databases, extensions, roles, etc..

- Documentation: https://registry.terraform.io/providers/Riskified/postgresql/latest/docs

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
Debug the Riskified Provider
---------------------------
in order to debug the provider, you need to setup a terraform environment  
Then you can use the following method:
1. Run the provider in debug mode in IntelliJ IDEA
2. In the debug console you will see an environment variable `TF_REATTACH_PROVIDERS` that you can copy and paste in the terminal to run the terraform environment
3. for example:
```sh   
TF_REATTACH_PROVIDERS='{"registry.terraform.io/Riskified/postgresql":{"Protocol":"grpc","ProtocolVersion":5,"Pid":88433,"Test":true,"Addr":{"Network":"unix","String":"/var/folders/x_/1k_vq74x3bl3xx4gnv8s1kn40000gn/T/plugin1848140345"}}}'
```
4. Run the Plan or apply. You can add breakpoints in the provider code and debug it while its doing the plan or apply


Building the terraform Riskified Provider
---------------------------
When the test are satisfied and before the merge you need to add a new git tag to the provider and push it to the repository
Goto the registry https://registry.terraform.io/providers/Riskified/postgresql/latest the tag should be the latest tag + 1. 
For example
```sh
git tag -a v1.34.0 -m"my new version"
