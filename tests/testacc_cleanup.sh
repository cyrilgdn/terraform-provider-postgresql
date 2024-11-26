#!/usr/bin/env bash

source "$(pwd)"/tests/switch_superuser.sh
docker compose -f "$(pwd)"/tests/docker-compose.yml down
unset TF_ACC PGHOST PGPORT PGUSER PGPASSWORD PGSSLMODE PGSUPERUSER
