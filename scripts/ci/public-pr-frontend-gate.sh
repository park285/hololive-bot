#!/usr/bin/env bash
set -euo pipefail

# 웹 배포 경계만 검사한다. 웹 소스·SSR·브라우저 검사는 iris-console이 소유한다.
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd -P)"
# 웹 credential·env 격리, 공유 docker-proxy와 30192 shortlink ingress를 검사한다.
cd "$root/deploy/compose"
docker compose -f docker-compose.prod.yml -f docker-compose.live-compat.yml \
  config --no-interpolate --no-env-resolution --format json |
  jq -e '
    (.services.deunhealth.environment.DOCKER_HOST == "tcp://docker-proxy:2375") and
    (.services["admin-dashboard-ingress"].network_mode == "host") and
    all(.services[];
      (.environment == null or
        ((.environment | type) == "array" and
          (.environment | all(.[]; type == "string" and (startswith("IRIS_ADMIN_WEB_") | not)))) or
        ((.environment | type) == "object" and
          (.environment | keys | all(.[]; startswith("IRIS_ADMIN_WEB_") | not)))) and
      all(.volumes[]?; .target != "/run/hololive-bot/iris-admin-credentials"))
  ' >/dev/null
grep -Eq 'listen @BIND_IP@:30192;' "$root/deploy/nginx/admin-dashboard-ingress.conf.template"
echo "[web-boundary] unified Iris app owns the web; central retains business H3 and shortlinks"
