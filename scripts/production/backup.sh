#!/usr/bin/env bash
source "$(dirname -- "$0")/common.sh"
[[ $# = 1 && "$1" = --writers-stopped ]] || fail 'Stop ALL application writers first, then run backup.sh --writers-stopped'
load_ops
lock_ops
backup="$GLEAN_BACKUP_ROOT/$(date -u +%Y%m%dT%H%M%SZ)-manual-$$"
backup_data "$backup"
printf 'Verified backup created: %s\n' "$backup"
