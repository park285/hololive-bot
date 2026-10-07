#!/usr/bin/env bash
# AP에서 검토된 이미지 태그로만 롤백한다. 원격 빌드는 하지 않는다.
set -euo pipefail
[[ $# -eq 7 ]] || { echo 'expected mode backup compose service container port and started_at' >&2; exit 2; }
mode="$1" backup="$2" ap_file="$3" service="$4" container="$5" port="$6" started_at="$7"
[[ "$mode" == check || "$mode" == apply ]] || exit 2
[[ "$backup" =~ ^backups/[A-Za-z0-9._-]+$ ]] || exit 2
[[ "$ap_file" =~ ^deploy/compose/docker-compose\.[A-Za-z0-9_-]+\.yml$ ]] || exit 2
[[ "$service" == youtube-collector-b && "$container" == hololive-youtube-collector-b && "$port" == 30015 ]] || exit 2
rollback_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
cd "$HOME/hololive-bot"
. "$rollback_root/scripts/deploy/lib/ap-prechange-config.sh"
. "$rollback_root/scripts/deploy/lib/po-sandbox-image.sh"
prod_file="deploy/compose/docker-compose.prod.yml"
old_prod="$backup/$prod_file.prechange"
old_ap="$backup/$ap_file.prechange"
prod_source="$(cat "$backup/prod-compose-source-path")"
ap_source="$(cat "$backup/ap-compose-source-path")"
# compose 파일은 deploy/compose 아래 한 경로만 쓴다. repo 루트 사본 폴백은 T18(2026-09-26)에서 지웠다
# (stack-audit T11 holo-ap-legacy-compose-path-fallback).
[[ "$prod_source" == "$prod_file" && "$ap_source" == "$ap_file" ]] || exit 2
compose=("$rollback_root/scripts/deploy/compose.sh" --project-directory "$HOME/hololive-bot/$(dirname "$prod_source")")
old_collector_tag=''
old_po_tag=''
po_state="$(cat "$backup/po-sandbox-prechange.state")"
[[ "$po_state" == present || "$po_state" == absent ]] || exit 1
[[ -r "$old_prod" && -r "$old_ap" ]] || exit 1
# rollback 기준점은 기록된 이전 collector image다. 퇴역 producer 상태 복원 경로는 T18(2026-09-26)에서 producer 런타임이
# 0개임을 확인해 지웠다(stack-audit T11 holo-collector-retired-producer-cutover-tooling). 기준점이 없는 백업은 되돌릴 이전
# collector가 없다는 뜻이라 거절한다.
if [[ ! -r "$backup/rollback-image-tag" ]]; then
  echo 'backup has no rollback-image-tag: no previous collector image to roll back to; fix forward' >&2
  exit 1
fi
old_collector_tag="$(cat "$backup/rollback-image-tag")"
[[ "$old_collector_tag" =~ ^hololive-youtube-collector:rollback-[0-9]{8}T[0-9]{6}Z$ ]] || exit 1
old_revision="$(sudo -n docker image inspect -f '{{index .Config.Labels "org.opencontainers.image.revision"}}' "$old_collector_tag")"
[[ "$old_revision" =~ ^[0-9a-f]{40}$ ]] || exit 1
python3 -c 'import json,sys; m=json.load(open(sys.argv[1])); assert m["source_revision"] == sys.argv[2] and m["go"]["goarch"] == "arm64" and m["image_id"] == sys.argv[3]' "$backup/collector-prechange-manifest.json" "$old_revision" "$(cat "$backup/collector-prechange.image-id")"
[[ "$(sudo -n docker image inspect -f '{{.Id}}' "$old_collector_tag")" == "$(cat "$backup/collector-prechange.image-id")" ]]
if [[ "$po_state" == present ]]; then
  old_po_tag="$(cat "$backup/rollback-po-image-tag")"
  [[ "$old_po_tag" =~ ^hololive-youtube-po-sandbox:rollback-[0-9]{8}T[0-9]{6}Z$ ]] || exit 1
  old_po_revision="$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["source_revision"])' "$backup/po-sandbox-prechange-manifest.json")"
  po_verify_image "$old_po_tag" "$backup/po-sandbox-prechange-manifest.json" "$old_po_revision" arm64 "$backup/po-sandbox-prechange.image-id"
else
  [[ ! -e "$backup/rollback-po-image-tag" && ! -e "$backup/po-sandbox-prechange-manifest.json" && ! -e "$backup/po-sandbox-prechange.image-id" ]] || exit 1
fi
sudo -n test -r /etc/stack-secrets/hololive-bot/ap-compose.env
sudo -n test -r /etc/stack-secrets/hololive-bot/youtube-collector.env
test -w /var/run/docker.sock || groups | grep -qw docker
preflight="$(mktemp -d)"
trap 'rm -rf "$preflight"' EXIT
mkdir -p "$preflight/deploy/compose"
cp "$old_prod" "$preflight/deploy/compose/docker-compose.prod.yml"
cp "$old_ap" "$preflight/deploy/compose/$(basename "$ap_file")"
ap_prechange_config sudo -n env COMPOSE_ENV_FILE=/etc/stack-secrets/hololive-bot/ap-compose.env COMPOSE_PROFILES=oracle \
  "${compose[@]}" -f "$preflight/deploy/compose/docker-compose.prod.yml" \
  -f "$preflight/deploy/compose/$(basename "$ap_file")" config --quiet
if [[ "$mode" == check ]]; then
  echo "AP rollback verified: backup=$backup collector=$old_collector_tag issuer=${old_po_tag:-absent}"
  exit 0
fi

# 두 서비스를 바꾸기 전에 이전 산출물과 설정을 모두 검증한다.
# 이전 VERSION과 실행 스크립트를 먼저 복원합니다. 후보 checkout의 버전으로 이전 이미지를 렌더링하지 않습니다.
python3 "$rollback_root/scripts/deploy/lib/ap-source-snapshot.py" restore "$HOME" "$HOME/hololive-bot/$backup"
compose=("$HOME/hololive-bot/scripts/deploy/compose.sh")
cmp -s "$old_prod" "$prod_source"
cmp -s "$old_ap" "$ap_source"
sudo -n docker tag "$old_collector_tag" hololive-youtube-collector:prod
# active receipt는 issuer를 멈추기 전에 같은 디렉터리의 rename으로 바꾼다. 기존 파일에 cp로 덮어쓰면
# 소유자가 다른 receipt에서 실패하고, 2026-10-02 seoul rollback은 issuer를 지운 뒤 이 단계에서 멈춰
# issuer 없이 남았다.
replace_receipt() {
  cp "$1" "$2.tmp"
  mv -f "$2.tmp" "$2"
}
if [[ "$po_state" == present ]]; then
  replace_receipt "$backup/po-sandbox-prechange-manifest.json" backups/po-sandbox-current-b.json
  replace_receipt "$backup/po-sandbox-prechange.image-id" backups/po-sandbox-current-b.image-id
fi
sudo -n docker stop hololive-youtube-po-b >/dev/null 2>&1 || true
sudo -n docker rm -f hololive-youtube-po-b >/dev/null 2>&1 || true
if [[ "$po_state" == present ]]; then
  sudo -n docker tag "$old_po_tag" hololive-youtube-po-sandbox:prod
  sudo -n env COMPOSE_ENV_FILE=/etc/stack-secrets/hololive-bot/ap-compose.env COMPOSE_PROFILES=oracle \
    "${compose[@]}" -f "$HOME/hololive-bot/$prod_source" -f "$HOME/hololive-bot/$ap_source" up -d --no-build --no-deps youtube-po-b
  for _ in $(seq 1 30); do
    [[ "$(docker inspect -f '{{if .State.Health}}{{.State.Health.Status}}{{end}}' hololive-youtube-po-b)" == healthy ]] && break
    sleep 2
  done
  [[ "$(docker inspect -f '{{.State.Health.Status}}' hololive-youtube-po-b)" == healthy ]]
  docker exec hololive-youtube-po-b /app/bin/po-broker --healthcheck --socket /run/hololive-youtube-po/worker.sock
else
  rm -f backups/po-sandbox-current-b.json backups/po-sandbox-current-b.image-id
  if sudo -n docker image inspect hololive-youtube-po-sandbox:prod >/dev/null 2>&1; then
    sudo -n docker image rm hololive-youtube-po-sandbox:prod >/dev/null
  fi
fi
sudo -n env COMPOSE_ENV_FILE=/etc/stack-secrets/hololive-bot/ap-compose.env COMPOSE_PROFILES=oracle \
  "${compose[@]}" -f "$HOME/hololive-bot/$prod_source" -f "$HOME/hololive-bot/$ap_source" up -d --no-build --no-deps --force-recreate "$service"
for _ in $(seq 1 30); do
  [[ "$(docker inspect -f '{{if .State.Health}}{{.State.Health.Status}}{{else}}{{.State.Status}}{{end}}' "$container")" == healthy ]] && break
  sleep 2
done
[[ "$(docker inspect -f '{{.State.Health.Status}}' "$container")" == healthy ]]
[[ "$(docker inspect -f '{{index .Config.Labels "org.opencontainers.image.revision"}}' "$container")" == "$old_revision" ]]
docker exec "$container" ./bin/healthcheck "https://127.0.0.1:$port/health" >/dev/null
printf 'AP rollback complete: backup=%s started_at=%s collector=%s issuer=%s\n' "$backup" "$started_at" "$old_collector_tag" "${old_po_tag:-absent}"
