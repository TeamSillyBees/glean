#!/usr/bin/env bash
source "$(dirname -- "$0")/common.sh"
[[ $# = 3 ]] || fail 'Usage: deploy.sh COMMIT_SHA BACKEND_IMAGE@sha256:... WEB_IMAGE@sha256:...'
validate_release "$@"
RELEASE_SHA="$1"
load_ops
activate migrate
