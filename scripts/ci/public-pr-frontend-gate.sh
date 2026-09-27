#!/usr/bin/env bash
set -euo pipefail

# 공개 frontend PR job은 이 저장소가 소유하는 웹 배포 경계만 검증합니다.
# 웹 소스·OpenAPI·SSR·브라우저 검사는 iris-admin/scripts/verify-all.sh가 소유합니다.
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd -P)"
# 퇴역 admin-dashboard 경로·서비스·네트워크와 30190/30191/ADMIN_MAINTENANCE 부재 가드(도입 d9acfb3ec)는
# DEC-20260926-hololive-retired-rollback-tooling에 따라 stack-audit 2026-09-26 T19(holo-api-admin-dashboard-retired-dir)에서
# 지웠다. 경로가 없는 상태로 v4.0.x가 배포됐고, 퇴역 서비스·Dockerfile 재도입은 test-three-runtime-topology.sh 정적 gate가 막는다.
# 아래는 현행 계약(웹 전용 credential·env 부재, 공유 docker-proxy, host network ingress의 30192 shortlink)만 검사한다.
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
