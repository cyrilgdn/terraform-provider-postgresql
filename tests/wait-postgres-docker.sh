#!/bin/bash

COMPOSE_FILE=${1:-"docker-compose.yml"}

TIMEOUT=30

if [ ! -e "$COMPOSE_FILE" ]; then
    echo "Unable to find docker-compose file: $COMPOSE_FILE"
    exit 1
fi

echo "Waiting for database to be up"
i=0
until docker-compose -f "$COMPOSE_FILE" logs postgres | grep "ready to accept connections" > /dev/null; do
    i=$((i + 1))
    if [ $i -eq $TIMEOUT ]; then
        echo
        echo "Timeout while waiting for database to be up"
        exit 1
    fi
    printf "."
    sleep 1
done
