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
docker compose -f docker-compose.prod.yml -f docker-compose.live-compat.yml \
  config --no-interpolate --no-env-resolution --format json |
  jq -e '
    (.services["admin-dashboard"] == null) and
    (.services["admin-docker-proxy"] == null) and
    (.networks["admin-docker-proxy-net"] == null) and
    (.services.deunhealth.environment.DOCKER_HOST == "tcp://docker-proxy:2375") and
    (.services["admin-dashboard-ingress"].network_mode == "host") and
    ((.services["admin-dashboard-ingress"].depends_on // {} | has("admin-dashboard")) | not) and
    all(.services[];
      (.environment == null or
        ((.environment | type) == "array" and
          (.environment | all(.[]; type == "string" and (startswith("IRIS_ADMIN_WEB_") | not)))) or
        ((.environment | type) == "object" and
          (.environment | keys | all(.[]; startswith("IRIS_ADMIN_WEB_") | not)))) and
      all(.volumes[]?; .target != "/run/hololive-bot/iris-admin-credentials"))
  ' >/dev/null
! grep -Eq '30190|30191|ADMIN_MAINTENANCE' "$root/deploy/nginx/admin-dashboard-ingress.conf.template"
grep -Eq 'listen @BIND_IP@:30192;' "$root/deploy/nginx/admin-dashboard-ingress.conf.template"
echo "[web-boundary] unified Iris app owns the web; central retains business H3 and shortlinks"
