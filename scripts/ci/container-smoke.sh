#!/usr/bin/env bash
# Run only disposable dependencies against the supplied local or published images.
set -euo pipefail
: "${BACKEND_IMAGE:?Set BACKEND_IMAGE to the image under test}"
: "${WEB_IMAGE:?Set WEB_IMAGE to the image under test}"
cd "$(dirname "${BASH_SOURCE[0]}")/../.."
umask 077
smoke="$(mktemp -d "${RUNNER_TEMP:-${TMPDIR:-/tmp}}/glean-container-smoke.XXXXXX")"
network="glean-smoke-${smoke##*.}"
cleanup() {
  result=$?
  trap - EXIT
  if (( result != 0 )); then
    for component in backend web postgres redis; do
      docker logs --tail 100 "$network-$component" 2>&1 || true
    done
  fi
  docker rm -fv "$network-web" "$network-backend" "$network-redis" "$network-postgres" 2>/dev/null || true
  docker network rm "$network" 2>/dev/null || true
  rm -rf "$smoke"
  exit "$result"
}
trap cleanup EXIT

docker run --rm --read-only --cap-drop ALL --security-opt no-new-privileges \
  --tmpfs /tmp:rw,noexec,nosuid,size=16m,mode=1777 "$WEB_IMAGE" -t

mkdir -p "$smoke/configs"
# Only committed base configuration; never setup output or a user local file.
cp configs/config.yaml configs/rbac_model.conf "$smoke/configs/"
chmod 755 "$smoke/configs"
chmod 644 "$smoke/configs/"*
db_password="$(openssl rand -hex 32)"
jwt_secret="$(openssl rand -hex 32)"
echo "::add-mask::$db_password"
echo "::add-mask::$jwt_secret"
printf 'POSTGRES_DB=glean_smoke\nPOSTGRES_USER=postgres\nPOSTGRES_PASSWORD=%s\n' \
  "$db_password" > "$smoke/postgres.env"
cat > "$smoke/backend.env" <<EOF
APP_ENV=local
INITRA_SERVER_ADDR=0.0.0.0:8080
INITRA_DATABASE_HOST=postgres
INITRA_DATABASE_DBNAME=glean_smoke
INITRA_DATABASE_PASSWORD=$db_password
INITRA_REDIS_ADDR=redis:6379
INITRA_TASK_REDIS_ADDR=redis:6379
INITRA_AUTH_JWT_SECRET=$jwt_secret
INITRA_IDGEN_NODE=1
INITRA_TASK_WORKER_ENABLED=false
INITRA_TASK_SCHEDULER_ENABLED=false
INITRA_MONITOR_SCHEDULER_ENABLED=false
EOF
# No dependency host ports or persistent volumes, and no application accounts.
docker network create "$network"
docker run -d --name "$network-postgres" --network "$network" --network-alias postgres \
  --env-file "$smoke/postgres.env" postgres:16
docker run -d --name "$network-redis" --network "$network" --network-alias redis \
  redis:7 redis-server --save '' --appendonly no
for attempt in {1..30}; do
  if docker exec "$network-postgres" pg_isready -h 127.0.0.1 -U postgres -d glean_smoke >/dev/null 2>&1 \
    && docker exec "$network-redis" redis-cli ping | grep -q PONG; then
    break
  fi
  sleep 2
done
docker exec "$network-postgres" pg_isready -h 127.0.0.1 -U postgres -d glean_smoke
docker exec "$network-redis" redis-cli ping | grep -q PONG
cat db/migrations/*.sql | docker exec -i "$network-postgres" \
  psql -U postgres -d glean_smoke --set ON_ERROR_STOP=1 --single-transaction > "$smoke/migrations.log"
docker run -d --name "$network-backend" --network "$network" --network-alias backend \
  --read-only --cap-drop ALL --security-opt no-new-privileges \
  --tmpfs /tmp:rw,noexec,nosuid,size=16m,mode=1777 \
  --tmpfs /app/var:rw,noexec,nosuid,size=32m,uid=65532,gid=65532,mode=0700 \
  --env-file "$smoke/backend.env" \
  --mount "type=bind,source=$smoke/configs,target=/app/configs,readonly" \
  -p 127.0.0.1:18080:8080 "$BACKEND_IMAGE"
# Docker can accept then reset TCP while the process is still bootstrapping.
# These idempotent health GETs retry all transport errors, within a hard budget.
curl --fail --silent --show-error --retry 30 --retry-delay 2 --retry-all-errors \
  --max-time 5 --retry-max-time 90 http://127.0.0.1:18080/ready > "$smoke/ready.json"


docker run -d --name "$network-web" --network "$network" \
  --read-only --cap-drop ALL --security-opt no-new-privileges \
  --tmpfs /tmp:rw,noexec,nosuid,size=16m,mode=1777 \
  -p 127.0.0.1:18081:8080 "$WEB_IMAGE"
curl --fail --silent --show-error --retry 15 --retry-delay 2 --retry-all-errors \
  --max-time 5 --retry-max-time 45 http://127.0.0.1:18081/healthz > /dev/null
python3 <<'PY'
import json
import re
from urllib.error import HTTPError
from urllib.request import urlopen

def get(base, path, expected=200):
    try:
        response = urlopen(base + path, timeout=10)
    except HTTPError as error:
        response = error
    with response:
        assert response.status == expected, (path, response.status, expected)
        return response.read(), response.headers

api = "http://127.0.0.1:18080"
web = "http://127.0.0.1:18081"
for path, status in [("/health", "ok"), ("/ready", "ready")]:
    body, _ = get(api, path)
    assert json.loads(body)["data"]["status"] == status
index, headers = get(web, "/")
assert b'<div id="root">' in index
assert headers["X-Content-Type-Options"] == "nosniff"
assert headers["X-Frame-Options"] == "DENY"
assert "frame-ancestors 'none'" in headers["Content-Security-Policy"]
assert get(web, "/papers/direct-navigation")[0] == index
assert get(web, "/healthz")[0] == index
assets = re.findall(rb'(?:src|href)="(/assets/[^"?]+\.(?:js|css))"', index)
assert any(path.endswith(b".js") for path in assets), "built JavaScript entry missing"
assert any(path.endswith(b".css") for path in assets), "built CSS entry missing"
for path in assets:
    body, asset_headers = get(web, path.decode())
    assert body and body != index
    assert "immutable" in asset_headers["Cache-Control"]
    assert "html" not in asset_headers["Content-Type"]
get(web, "/assets/does-not-exist.js", 404)
get(web, "/api", 404)
for base in (api, web):
    body, api_headers = get(base, "/api/v1/catalog/papers", 401)
    assert "json" in api_headers["Content-Type"]
    assert isinstance(json.loads(body), dict), "API errors must not become SPA HTML"
print("Container HTTP smoke passed: nonroot static SPA/assets, backend dependencies and unauthenticated API proxy.")
PY
