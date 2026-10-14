#!/bin/sh
# Runs once, only when the Postgres data volume is empty (first start).
# Creates the restricted login role the API uses at runtime.
# Table privileges and RLS policies for it are granted by the migrations.
set -eu

psql -v ON_ERROR_STOP=1 \
    --username "$POSTGRES_USER" --dbname "$POSTGRES_DB" \
    -v app_password="$APP_DB_PASSWORD" <<'EOSQL'
CREATE ROLE mscs_app LOGIN PASSWORD :'app_password'
    NOSUPERUSER NOCREATEDB NOCREATEROLE NOBYPASSRLS;
EOSQL
