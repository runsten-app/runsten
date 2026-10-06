#!/bin/sh
# First start of the PostgreSQL container only (empty volume): creates the user
# Runsten connects with, and its database. The user owns the database, so it can apply
# the migrations, but it is not a superuser. The RLS role runsten_app is created here
# with the admin option for that user; the migrations would otherwise need CREATEROLE.
set -eu
: "${RUNSTEN_DB_PASSWORD:?RUNSTEN_DB_PASSWORD is required}"
psql -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" --dbname postgres \
	-v password="$RUNSTEN_DB_PASSWORD" <<'SQL'
CREATE ROLE runsten_app NOLOGIN;
CREATE ROLE runsten LOGIN PASSWORD :'password';
GRANT runsten_app TO runsten WITH ADMIN OPTION;
CREATE DATABASE runsten OWNER runsten;
SQL
