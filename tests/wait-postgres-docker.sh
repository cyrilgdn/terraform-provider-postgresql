#!/bin/bash

TIMEOUT=${TIMEOUT:-30}
export PGCONNECT_TIMEOUT=1

echo "Waiting for database to be up"

until [ $SECONDS -ge  $TIMEOUT ]; do
    if psql -c "SELECT 1" > /dev/null 2>&1 ; then
        printf '\nDatabase is ready'
        exit 0
    fi
    printf "."
    sleep 1
done
printf '\nTimeout while waiting for database to be up'
exit 1
