#!/bin/bash

source "$(pwd)"/tests/env.sh
docker-compose -f "$(pwd)"/tests/docker-compose.yml up -d
"$(pwd)"/tests/wait-postgres-docker.sh "$(pwd)"/tests/docker-compose.yml
