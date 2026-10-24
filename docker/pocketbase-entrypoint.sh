#!/bin/sh
# Personal install: one superuser, migrations, and the console from pb_public.
set -eu
if [ -z "${ADMIN_EMAIL:-}" ] || [ -z "${ADMIN_PASSWORD:-}" ]; then
  echo "ADMIN_EMAIL and ADMIN_PASSWORD are required" >&2
  exit 1
fi
pocketbase superuser upsert "$ADMIN_EMAIL" "$ADMIN_PASSWORD" --dir /pb_data --migrationsDir /pb_migrations
exec pocketbase serve --http 0.0.0.0:8090 --dir /pb_data --migrationsDir /pb_migrations --publicDir /pb_public
