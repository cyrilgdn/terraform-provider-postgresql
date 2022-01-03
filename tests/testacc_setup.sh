#!/bin/bash

source "$(pwd)"/tests/switch_superuser.sh
docker compose -f "$(pwd)"/tests/docker-compose.yml up -d --wait
