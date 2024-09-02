#!/bin/bash

export TF_ACC=true
export PGHOST=postgres
export PGPORT=5432
export PGUSER=postgres
export PGPASSWORD=postgres
export PGSSLMODE=disable
export PGSUPERUSER=true
export PGPROXY=socks5://127.0.0.1:11080
