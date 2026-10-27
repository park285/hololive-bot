#!/usr/bin/env bash
set -euo pipefail

# 웹 배포 경계만 검사한다. 웹 소스·SSR·브라우저 검사는 iris-console이 소유한다.
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd -P)"
# 공유 docker-proxy 연결과 30192 shortlink ingress를 검사한다.
cd "$root/deploy/compose"
docker compose -f docker-compose.prod.yml -f docker-compose.live-compat.yml \
  config --no-interpolate --no-env-resolution --format json |
  jq -e '
    (.services.deunhealth.environment.DOCKER_HOST == "tcp://docker-proxy:2375") and
    (.services["admin-dashboard-ingress"].network_mode == "host")
  ' >/dev/null
grep -Eq 'listen @BIND_IP@:30192;' "$root/deploy/nginx/admin-dashboard-ingress.conf.template"
echo "[web-boundary] unified Iris app owns the web; central retains business H3 and shortlinks"
