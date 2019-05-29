#!/bin/bash

source "$(pwd)"/tests/env.sh
docker-compose -f "$(pwd)"/tests/docker-compose.yml down
unset TF_ACC PGHOST PGPORT PGUSER PGPASSWORD PGSSLMODE