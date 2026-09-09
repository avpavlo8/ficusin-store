#!/usr/bin/env bash
# Restore a PostgreSQL custom-format backup into a disposable database and
# compare the commerce tables. The target name guard makes accidental use
# against a real database fail closed.
set -euo pipefail

if [[ "$#" != "2" ]]; then
  echo 'usage: verify-backup-restore.sh SOURCE_DATABASE_URL RESTORE_DATABASE_URL' >&2
  exit 64
fi
source_url="$1"
restore_url="$2"
source_name="$(psql "$source_url" -Atc 'SELECT current_database()')"
restore_name="$(psql "$restore_url" -Atc 'SELECT current_database()')"
if [[ "$source_name" == "$restore_name" ]]; then
  echo 'Source and restore databases must be different.' >&2
  exit 64
fi
if [[ "$restore_name" != ficusin_restore_* ]]; then
  echo "Refusing restore into database without ficusin_restore_ prefix: ${restore_name}" >&2
  exit 64
fi
target_tables="$(psql "$restore_url" -Atc "SELECT COUNT(*) FROM pg_tables WHERE schemaname='public'")"
if [[ "$target_tables" != "0" ]]; then
  echo "Restore target must be empty; found ${target_tables} public tables." >&2
  exit 64
fi

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
started="$(date +%s)"
pg_dump --format=custom --no-owner --no-privileges --file="$work/backup.dump" "$source_url"
pg_restore --exit-on-error --no-owner --no-privileges --dbname="$restore_url" "$work/backup.dump"

for table in orders order_items payments inventory outbox consent_events; do
  source_signature="$(psql "$source_url" -Atc "SELECT COUNT(*)::text || ':' || COALESCE(SUM(id),0)::text FROM ${table}")"
  restore_signature="$(psql "$restore_url" -Atc "SELECT COUNT(*)::text || ':' || COALESCE(SUM(id),0)::text FROM ${table}")"
  if [[ "$source_signature" != "$restore_signature" ]]; then
    echo "Restore verification failed for ${table}: source=${source_signature} restore=${restore_signature}." >&2
    exit 1
  fi
done

source_schema="$(psql "$source_url" -Atc "SELECT md5(string_agg(name, ',' ORDER BY name)) FROM schema_migrations")"
restore_schema="$(psql "$restore_url" -Atc "SELECT md5(string_agg(name, ',' ORDER BY name)) FROM schema_migrations")"
if [[ "$source_schema" != "$restore_schema" ]]; then
  echo 'Restored migration ledger differs from the source.' >&2
  exit 1
fi
elapsed="$(( $(date +%s) - started ))"
bytes="$(wc -c <"$work/backup.dump" | tr -d ' ')"
echo "restore_verified source=${source_name} target=${restore_name} bytes=${bytes} rto_seconds=${elapsed}"
