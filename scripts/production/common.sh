#!/usr/bin/env bash
# Sourced by the entrypoints. Never enable xtrace: the environment contains secrets.
set -euo pipefail
set +x
umask 077
ROOT="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd)"
fail() { printf '%s\n' "$*" >&2; exit 1; }
need() { command -v "$1" >/dev/null || fail "Required tool is unavailable: $1"; }
load_ops() {
  need python3
  : "${PRODUCTION_BASE_DIR:?Set the absolute server-local deployment base directory}"
  export PRODUCTION_BASE_DIR
  python3 "$ROOT/scripts/production/guard.py" layout
  local requested_base="$PRODUCTION_BASE_DIR"
  local config="$PRODUCTION_BASE_DIR/config/ops.env"
  python3 "$ROOT/scripts/production/guard.py" private-file "$config"
  set -a
  # Operator-provisioned shell configuration is trusted code, never a release artifact.
  source "$config"
  set +a
  [[ "$PRODUCTION_BASE_DIR" = "$requested_base" ]] || fail 'ops.env may not change PRODUCTION_BASE_DIR.'
  python3 "$ROOT/scripts/production/guard.py" layout
  export GLEAN_OPS_ENV="$config"
  export GLEAN_BACKEND_ENV="$PRODUCTION_BASE_DIR/config/backend.env"
  export GLEAN_CONFIG_DIR="$PRODUCTION_BASE_DIR/config/configs"
  export GLEAN_DATA_DIR="$PRODUCTION_BASE_DIR/data"
  export GLEAN_ORIGINAL_ARCHIVE_DIR="$PRODUCTION_BASE_DIR/originals"
  export GLEAN_BACKUP_ROOT="$PRODUCTION_BASE_DIR/backups"
  export GLEAN_STATE_DIR="$PRODUCTION_BASE_DIR/state"
  [[ -d "$GLEAN_DATA_DIR" && -d "$GLEAN_ORIGINAL_ARCHIVE_DIR" ]] || fail 'Persistent data and original private archive directories must already exist.'
  python3 "$ROOT/scripts/production/guard.py" ops-paths
  mkdir -p -- "$GLEAN_BACKUP_ROOT" "$GLEAN_STATE_DIR" "$PRODUCTION_BASE_DIR/recovery"
}

lock_ops() {
  need flock
  python3 "$ROOT/scripts/production/guard.py" layout
  exec 9>"$GLEAN_STATE_DIR/operations.lock"
  flock -n 9 || fail 'Another deployment or backup operation is running.'
}
validate_database() {
  python3 "$ROOT/scripts/production/guard.py" database "$1"
}
validate_release() {
  [[ "$1" =~ ^[0-9a-f]{40}$ ]] || fail 'A full lowercase commit SHA is required.'
  [[ -f "$ROOT/deploy/REVISION" && "$(cat "$ROOT/deploy/REVISION")" = "$1" ]] || fail 'Release revision does not match requested commit.'
  [[ "$2" =~ ^[a-z0-9][a-z0-9./:_-]*@sha256:[0-9a-f]{64}$ ]] || fail 'Backend image must be pinned by SHA256 digest.'
  [[ "$3" =~ ^[a-z0-9][a-z0-9./:_-]*@sha256:[0-9a-f]{64}$ ]] || fail 'Web image must be pinned by SHA256 digest.'
  export GLEAN_BACKEND_IMAGE="$2" GLEAN_WEB_IMAGE="$3"
}
pg_client() {
  python3 "$ROOT/scripts/production/guard.py" libpq "$@"
}
compose() {
  python3 "$ROOT/scripts/production/guard.py" compose "$ROOT" "$@"
}
backup_data() {
  need pg_dump; need pg_restore; need tar; need sha256sum
  validate_database DATABASE_URL
  local destination="$1"
  python3 "$ROOT/scripts/production/guard.py" backup-path "$destination"
  mkdir -m 700 -- "$destination"
  # Full database, including audit overlays, source evidence and Atlas revisions.
  pg_client DATABASE_URL pg_dump --format=custom --no-owner --no-privileges --file="$destination/database.dump" 2>"$destination/database.log"
  pg_client DATABASE_URL pg_restore --list "$destination/database.dump" >"$destination/database.toc"
  tar -C "$GLEAN_DATA_DIR" -czf "$destination/data.tar.gz" . 2>"$destination/archive.log"
  tar -C "$GLEAN_ORIGINAL_ARCHIVE_DIR" -czf "$destination/originals.tar.gz" . 2>>"$destination/archive.log"
  python3 "$ROOT/scripts/production/guard.py" validate-archive "$destination/data.tar.gz"
  python3 "$ROOT/scripts/production/guard.py" validate-archive "$destination/originals.tar.gz"
  printf '%s\n' "${RELEASE_SHA:-manual-backup}" > "$destination/REVISION"
  (cd "$destination" && sha256sum database.dump data.tar.gz originals.tar.gz REVISION > SHA256SUMS)
  touch "$destination/COMPLETE"
}
ready() {
  local attempt
  for attempt in $(seq 1 30); do
    if curl --fail --silent --max-time 3 http://127.0.0.1:8080/ready >/dev/null && \
       curl --fail --silent --max-time 3 http://127.0.0.1:3000/healthz >/dev/null; then
      return 0
    fi
    sleep 3
  done
  return 1
}
activate() {
  local mode="$1"
  need docker; need curl; need pg_dump; need pg_restore; need tar; need sha256sum
  [[ "${GLEAN_ALLOW_LOCAL_TEST_DB:-0}" = 0 ]] || fail 'Test database mode is prohibited for deployment.'
  validate_database DATABASE_URL
  python3 "$ROOT/scripts/production/guard.py" release-path "$ROOT"
  : "${GLEAN_BACKEND_ENV:?}" "${GLEAN_CONFIG_DIR:?}"
  [[ -f "$GLEAN_BACKEND_ENV" && -d "$GLEAN_CONFIG_DIR" ]] || fail 'Server-local backend configuration is missing.'
  python3 "$ROOT/scripts/production/guard.py" private-file "$GLEAN_BACKEND_ENV"
  [[ "$mode" != migrate ]] || need atlas
  lock_ops
  python3 "$ROOT/scripts/production/guard.py" check-current "$GLEAN_STATE_DIR/current.json"
  python3 "$ROOT/scripts/production/guard.py" active-mounts
  compose config --quiet
  compose pull
  local backup="$GLEAN_BACKUP_ROOT/$(date -u +%Y%m%dT%H%M%SZ)-$RELEASE_SHA-$$"
  printf 'Stopping application writers before backup.\n'
  compose stop backend
  trap 'status=$?; if (( status != 0 )); then compose stop backend >/dev/null 2>&1 || true; fi' EXIT
  # No EXIT trap restarts writers: a failed backup/migration remains in maintenance.
  backup_data "$backup"
  printf 'Verified backup created: %s\n' "$backup"
  if [[ "$mode" = migrate ]]; then
    (cd "$ROOT" && atlas -c file://db/atlas.hcl migrate apply --env prod) >"$backup/migration.log" 2>&1 || fail "Migration failed; writers remain stopped. Inspect private log in $backup."
  fi
  compose up -d --no-build
  if ! ready; then
    compose stop backend
    fail 'Readiness failed; backend stopped. Review schema compatibility before manual code rollback.'
  fi
  python3 "$ROOT/scripts/production/guard.py" layout
  if [[ -f "$GLEAN_STATE_DIR/current.json" ]]; then
    cp -- "$GLEAN_STATE_DIR/current.json" "$GLEAN_STATE_DIR/previous.json"
  fi
  export RELEASE_SHA ROOT
  python3 "$ROOT/scripts/production/guard.py" record-release "$GLEAN_STATE_DIR/current.json"
  trap - EXIT
  printf 'Deployment ready: %s\n' "$RELEASE_SHA"
}
