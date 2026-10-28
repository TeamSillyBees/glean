#!/usr/bin/env bash
source "$(dirname -- "$0")/common.sh"
[[ $# = 4 && "$4" = --confirm-schema-compatible ]] || fail 'Usage: rollback.sh COMMIT_SHA BACKEND_IMAGE@sha256:... WEB_IMAGE@sha256:... --confirm-schema-compatible'
# Run this script FROM the retained, verified old release. Never migrate backwards.
validate_release "$1" "$2" "$3"
RELEASE_SHA="$1"
load_ops
activate rollback
