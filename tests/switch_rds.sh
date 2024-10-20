#!/bin/bash

HERE=$(dirname $(readlink -f ${BASH_SOURCE:-${(%):-%N}}))

source "$HERE/switch_superuser.sh"

echo "Switching to an RDS-like environment"
psql -d postgres  > /dev/null <<EOS
DO
\$BODY\$
    DECLARE
        server_version INT = 0;
    BEGIN
        CREATE ROLE rds LOGIN CREATEDB CREATEROLE PASSWORD 'rds';
        -- On RDS, postgres user is member of these roles
        -- But it's not really needed for the tests and pg_monitor is
        -- not available on Posgres 8.x
        -- GRANT pg_monitor,pg_signal_backend TO rds;
        ALTER DATABASE postgres OWNER TO rds;
        ALTER SCHEMA public OWNER TO rds;
        SELECT setting FROM pg_settings WHERE name = 'server_version_num' INTO server_version;
        IF server_version >= 160000 THEN
            ALTER ROLE rds SET createrole_self_grant TO 'set, inherit';
        END IF;
    END;
\$BODY\$;
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
