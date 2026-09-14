#!/usr/bin/env bash
set -euo pipefail

# 공개 frontend PR job은 이 저장소가 소유하는 웹 배포 경계만 검증합니다.
# 웹 소스·OpenAPI·SSR·브라우저 검사는 iris-admin/scripts/verify-all.sh가 소유합니다.
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd -P)"
for retired in admin-dashboard/backend admin-dashboard/frontend admin-dashboard/Dockerfile; do
  [[ ! -e "$root/$retired" ]] || {
    echo "web implementation must be owned by iris-admin: $retired" >&2
    exit 1
  }
done
cd "$root/deploy/compose"
docker compose -f docker-compose.prod.yml -f docker-compose.admin-security.yml \
  config --no-interpolate --no-env-resolution --format json |
  jq -e '
    (.services["admin-dashboard"]) as $web |
    ($web.build == null) and ($web.env_file == null or $web.env_file == []) and
    ($web.read_only == true) and
    ($web.environment.IRIS_ADMIN_WEB_SURFACE == "hololive") and
    ($web.environment.IRIS_ADMIN_WEB_BIND == "0.0.0.0:30190") and
    ($web.environment.IRIS_ADMIN_WEB_TRUSTED_PROXY == "172.23.0.1") and
    ($web.environment.IRIS_ADMIN_WEB_HOLOLIVE_CA_FILE == "/run/hololive-bot/certs/iris-ca.pem") and
    ($web.environment.CREDENTIALS_DIRECTORY == "/run/hololive-bot/iris-admin-credentials") and
    (($web.environment | keys | sort) == ([
      "CREDENTIALS_DIRECTORY", "IRIS_ADMIN_WEB_BIND", "IRIS_ADMIN_WEB_SURFACE",
      "IRIS_ADMIN_WEB_TRUSTED_PROXY", "IRIS_ADMIN_WEB_ORIGIN", "IRIS_ADMIN_WEB_USER_LOGIN",
      "IRIS_ADMIN_WEB_HOLOLIVE_ORIGIN", "IRIS_ADMIN_WEB_HOLOLIVE_CA_FILE",
      "IRIS_ADMIN_WEB_TEST_ACCOUNT_DIR"
    ] | sort)) and
    (($web.networks | keys) == ["hololive-net"]) and
    (($web.depends_on | keys) == ["hololive-api"]) and
    ($web.environment.IRIS_ADMIN_WEB_TEST_ACCOUNT_DIR == "/run/hololive-bot/test-account") and
    (all($web.ports[]; .host_ip == "127.0.0.1" and .target == 30190)) and
    (all($web.volumes[]; .read_only == true)) and
    (($web.volumes | map(.target) | sort) == [
      "/run/hololive-bot/certs/iris-ca.pem", "/run/hololive-bot/iris-admin-credentials"
    ]) and
    (.services["admin-docker-proxy"] == null) and
    (.networks["admin-docker-proxy-net"] == null) and
    (.services.deunhealth.environment.DOCKER_HOST == "tcp://docker-proxy:2375")
  ' >/dev/null
echo "[web-boundary] Iris-owned image, Holo-only credentials and no Docker/host resource capability"
