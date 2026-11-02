#!/usr/bin/env bash
# Explicit isolated CI rehearsal only. Never accepts the production DATABASE_URL.
set -euo pipefail
set +x
umask 077
ROOT="$(cd -- "$(dirname -- "$0")/../../.." && pwd)"
: "${GLEAN_RECOVERY_CI_DATABASE_URL:?Set an isolated loopback PostgreSQL CI admin connection}"
export GLEAN_ALLOW_LOCAL_TEST_DB=1
python3 "$ROOT/scripts/production/guard.py" database GLEAN_RECOVERY_CI_DATABASE_URL
# Test mode must actually be loopback, even when a verify-full URL is provided.
python3 - <<'PY'
import os
from urllib.parse import urlsplit
u = urlsplit(os.environ['GLEAN_RECOVERY_CI_DATABASE_URL'])
if u.hostname not in ('127.0.0.1', 'localhost', '::1'):
    raise SystemExit('Recovery drill requires an isolated loopback CI database.')
PY
for tool in psql pg_dump pg_restore; do command -v "$tool" >/dev/null; done
work="$(mktemp -d)"
prefix="glean_recovery_$(date +%s)_$$"
source_db="${prefix}_source"
restore_db="${prefix}_restore"
pg_client() {
  python3 "$ROOT/scripts/production/guard.py" libpq "$@"
}
cleanup() {
  pg_client GLEAN_RECOVERY_CI_DATABASE_URL psql -X --set=ON_ERROR_STOP=1 -c "DROP DATABASE IF EXISTS $restore_db" >/dev/null 2>&1 || true
  pg_client GLEAN_RECOVERY_CI_DATABASE_URL psql -X --set=ON_ERROR_STOP=1 -c "DROP DATABASE IF EXISTS $source_db" >/dev/null 2>&1 || true
  rm -rf -- "$work"
}
trap cleanup EXIT
pg_client GLEAN_RECOVERY_CI_DATABASE_URL psql -X -q --set=ON_ERROR_STOP=1 -c "CREATE DATABASE $source_db"
pg_client GLEAN_RECOVERY_CI_DATABASE_URL psql -X -q --set=ON_ERROR_STOP=1 -c "CREATE DATABASE $restore_db"
# URL manipulation stays in memory; never print credentials.
export source_db restore_db work
python3 - <<'PY'
import os, shlex
from pathlib import Path
from urllib.parse import urlsplit, urlunsplit
u = urlsplit(os.environ['GLEAN_RECOVERY_CI_DATABASE_URL'])
def db(name): return urlunsplit(u._replace(path='/' + os.environ[name]))
w = Path(os.environ['work'])
values = {'DATABASE_URL': db('source_db'), 'GLEAN_RESTORE_DATABASE_URL': db('restore_db'),
          'GLEAN_DATA_DIR': str(w/'data'), 'GLEAN_ORIGINAL_ARCHIVE_DIR': str(w/'originals'),
          'GLEAN_BACKUP_ROOT': str(w/'backups'), 'GLEAN_STATE_DIR': str(w/'state'),
          'GLEAN_ALLOW_LOCAL_TEST_DB': '1'}
(w/'config').mkdir(mode=0o700)
(w/'config/ops.env').write_text(''.join(k+'='+shlex.quote(v)+'\n' for k,v in values.items()))
(w/'config/ops.env').chmod(0o600)
PY
export PRODUCTION_BASE_DIR="$work"
export GLEAN_OPS_ENV="$work/config/ops.env"
set -a; source "$GLEAN_OPS_ENV"; set +a
mkdir -p "$GLEAN_DATA_DIR/uploads" "$GLEAN_ORIGINAL_ARCHIVE_DIR"
printf 'uploaded-original-user-asset\n' > "$GLEAN_DATA_DIR/uploads/sentinel.txt"
printf 'PRIVATE user archive, not the sanitized Git baseline\n' > "$GLEAN_ORIGINAL_ARCHIVE_DIR/original-library.md"
for migration in "$ROOT"/db/migrations/*.sql; do pg_client DATABASE_URL psql -X -q --set=ON_ERROR_STOP=1 --single-transaction -f "$migration"; done
pg_client DATABASE_URL psql -X -q --set=ON_ERROR_STOP=1 --single-transaction -f "$ROOT/scripts/production/tests/fixture.sql"
# Canonical JSON captures every populated table and every column, including hashes,
# review/correction history, raw archives, observation payloads, monitor state and IDs.
snapshot() {
  pg_client "$1" psql -X -At --set=ON_ERROR_STOP=1 <<'SQL'
SELECT format('SELECT jsonb_build_object(''table'', %L, ''rows'', COALESCE(jsonb_agg(to_jsonb(t) ORDER BY to_jsonb(t)::text), ''[]''::jsonb))::text FROM %I.%I t;', schemaname || '.' || tablename, schemaname, tablename)
FROM pg_tables WHERE schemaname NOT IN ('pg_catalog', 'information_schema') ORDER BY schemaname, tablename
\gexec
SQL
}
snapshot DATABASE_URL > "$work/before.jsonl"
bash "$ROOT/scripts/production/backup.sh" --writers-stopped
backup="$(find "$GLEAN_BACKUP_ROOT" -mindepth 1 -maxdepth 1 -type d)"
bash "$ROOT/scripts/production/restore.sh" "$backup" "$work/recovery/restored" --confirm-empty-target
snapshot GLEAN_RESTORE_DATABASE_URL > "$work/after.jsonl"
cmp "$work/before.jsonl" "$work/after.jsonl"
cmp "$GLEAN_DATA_DIR/uploads/sentinel.txt" "$work/recovery/restored/data/uploads/sentinel.txt"
cmp "$GLEAN_ORIGINAL_ARCHIVE_DIR/original-library.md" "$work/recovery/restored/originals/original-library.md"
# A repeated restore must refuse the populated DB, not overwrite application state.
if bash "$ROOT/scripts/production/restore.sh" "$backup" "$work/recovery/repeated" --confirm-empty-target >"$work/repeated.log" 2>&1; then
  echo 'ERROR: non-empty restore target was accepted.' >&2; exit 1
fi
grep -q 'not empty' "$work/repeated.log"
test ! -e "$work/recovery/repeated"
snapshot GLEAN_RESTORE_DATABASE_URL > "$work/after-refusal.jsonl"
cmp "$work/after.jsonl" "$work/after-refusal.jsonl"
printf 'PASS: full real-schema PostgreSQL roundtrip, user/review/correction/original archives/uploads/monitor/migration state, and non-empty-target refusal.\n'
