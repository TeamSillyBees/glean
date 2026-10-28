#!/usr/bin/env bash
source "$(dirname -- "$0")/common.sh"
[[ $# = 3 && "$3" = --confirm-empty-target ]] || fail 'Usage: restore.sh BACKUP_DIRECTORY NEW_RECOVERY_DIRECTORY --confirm-empty-target'
backup="$(cd -- "$1" && pwd)"
target="$2"
[[ "$target" = /* && ! -e "$target" ]] || fail 'New data directory must be an absolute, absent path.'
load_ops
python3 "$ROOT/scripts/production/guard.py" restore-target "$target"
lock_ops
validate_database GLEAN_RESTORE_DATABASE_URL
python3 "$ROOT/scripts/production/guard.py" distinct-restore-database
need psql; need pg_restore; need sha256sum
[[ -f "$backup/COMPLETE" ]] || fail 'Backup is incomplete.'
# Never accept attacker-controlled checksum paths or missing entries.
python3 - "$backup/SHA256SUMS" <<'PY'
import re, sys
lines = open(sys.argv[1], encoding='utf-8').read().splitlines()
expected = ['database.dump', 'data.tar.gz', 'originals.tar.gz', 'REVISION']
if len(lines) != 4 or any(not re.fullmatch(r'[0-9a-f]{64}  ' + re.escape(name), line) for line, name in zip(lines, expected)):
    raise SystemExit('Invalid backup checksum manifest.')
PY
(cd "$backup" && sha256sum --check --status SHA256SUMS) || fail 'Backup checksum verification failed.'
# A separate newly created DB is required. No DROP, --clean, or production replacement.
objects="$(pg_client GLEAN_RESTORE_DATABASE_URL psql -X -At --set=ON_ERROR_STOP=1 -c "SELECT count(*) FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname NOT IN ('pg_catalog','information_schema') AND n.nspname NOT LIKE 'pg_toast%' AND c.relkind IN ('r','p','v','m','S','f');")"
[[ "$objects" = 0 ]] || fail 'Restore target is not empty; no changes made.'
# Verify archive paths BEFORE changing the DB. Files stay in a new isolated directory.
python3 "$ROOT/scripts/production/guard.py" validate-archive "$backup/data.tar.gz"
python3 "$ROOT/scripts/production/guard.py" validate-archive "$backup/originals.tar.gz"
mkdir -m 700 -- "$target"
python3 "$ROOT/scripts/production/guard.py" extract "$backup/data.tar.gz" "$target/data"
python3 "$ROOT/scripts/production/guard.py" extract "$backup/originals.tar.gz" "$target/originals"
# Empty -d is intentional: pg_restore selects DB mode, then libpq ignores the
# empty parameter and reads the decoded database NAME supplied by guard.libpq.
# See PostgreSQL 16 pg_restore.c useDB and libpq PQconnectdbParams defaults.
pg_client GLEAN_RESTORE_DATABASE_URL pg_restore --exit-on-error --single-transaction --no-owner --no-privileges --dbname="" "$backup/database.dump" 2>"$target/restore.log" || fail 'Restore failed; DB transaction rolled back. Discard isolated target after inspection; production was not changed.'
printf 'Restored isolated database and files. Verify records, ownership and monitor recovery before any traffic switch.\n'
