#!/bin/bash

export TF_ACC=true
export PGHOST=postgres
export PGPORT=5432
export PGUSER=postgres
export PGPASSWORD=postgres
export PGSSLMODE=disable
export PGSUPERUSER=true
export JUMPHOST=localhost
export JUMPHOST_PORT=2222
export JUMPHOST_USER=tunnel
export JUMPHOST_LOCALPORT=15432

eval "$(ssh-agent -s)"
cat tests/keys/private/id_rsa | ssh-add -