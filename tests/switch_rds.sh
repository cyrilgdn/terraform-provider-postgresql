#!/bin/bash

HERE=$(dirname $(readlink -f ${BASH_SOURCE:-${(%):-%N}}))

source "$HERE/switch_superuser.sh"

echo "Switching to an RDS-like environment"
psql -d postgres  > /dev/null <<EOS
BEGIN;
    CREATE role rds LOGIN CREATEDB CREATEROLE PASSWORD 'rds';
    -- On RDS, postgres user is member of these roles
    -- But it's not really needed for the tests and pg_monitor is
    -- not available on Postgres 8.x
    -- GRANT pg_monitor,pg_signal_backend TO rds;
    ALTER DATABASE postgres OWNER TO rds;
    ALTER SCHEMA public OWNER TO rds;
COMMIT;
EOS

psql -d template1 > /dev/null <<EOS
BEGIN;
    ALTER SCHEMA public OWNER TO rds;
COMMIT;
EOS

export TF_ACC=true
export PGHOST=localhost
export PGPORT=25432
export PGUSER=rds
export PGPASSWORD=rds
export PGSSLMODE=disable
export PGSUPERUSER=false
