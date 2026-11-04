#!/usr/bin/env bash
# quadlet-redeploy-service.sh가 release 디렉터리에 보존한 이전 image·worker profile pair로 중앙
# hololive-api·hololive-alarm-worker를 되돌린다. Docker 시절 compose-rollback-service.sh와 같은 경계로
# DB migration·schema·secret mirror는 되돌리지 않는다. 호환성 판정은 docs/current/runbooks/rollback.md를 따른다.
#   --check(기본): pair checksum·image identity 대조와 이전 image의 --check-worker-profile(network none)만 한다.
#   --apply: 이전 pair drop-in을 게시하고 daemon-reload 뒤 unit을 재시작해 실행 중 profile과 NRestarts를 확인한다.
# quadlet-redeploy-service.sh는 활성화 뒤 실패하면 이 스크립트를 --apply로 한 번 호출한다.
set -Eeuo pipefail

die() { echo "[quadlet-rollback] $*" >&2; exit 1; }
usage() { echo "Usage: $0 <release-dir> [--check|--apply]" >&2; exit 2; }

[[ $# -ge 1 && $# -le 2 ]] || usage
mode="${2:---check}"
[[ "$mode" == --check || "$mode" == --apply ]] || usage

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd -P)"
# 운영(root)은 경로를 고정하고 배포 트리의 사본만 실행한다. 테스트만 비root로 HOLOLIVE_QUADLET_TEST_ROOT 아래를 쓴다
# (quadlet-prestart.sh·worker-profile-pair.py와 같은 접두어).
if [[ "${EUID:-$(id -u)}" -eq 0 ]]; then
  [[ -z "${HOLOLIVE_QUADLET_TEST_ROOT:-}" ]] || die "HOLOLIVE_QUADLET_TEST_ROOT is only for non-root tests"
  prefix=""
else
  prefix="${HOLOLIVE_QUADLET_TEST_ROOT:-}"
  [[ -n "$prefix" ]] || die "must run as root (sudo)"
fi
deploy_root="$(realpath -m -- "$prefix/opt/hololive-bot/compose/current")"
fence_dir="$prefix/var/lib/hololive-postgres-fence"
if [[ -z "$prefix" && "$ROOT_DIR" != "$deploy_root" ]]; then
  die "run the deployed copy: $deploy_root/scripts/deploy/quadlet-rollback-service.sh"
fi
pair_tool="$ROOT_DIR/scripts/deploy/lib/worker-profile-pair.py"

release="$(realpath -e -- "$1")" || die "release directory not found: $1"
[[ "$release" == "$deploy_root/artifacts/worker-profile/releases/"* ]] ||
  die "release must live under $deploy_root/artifacts/worker-profile/releases"
service="$(<"$release/service")"
case "$service" in
  hololive-api|hololive-alarm-worker) ;;
  *) die "unsupported service recorded in $release/service" ;;
esac
unit="$service.service"
previous="$release/previous"
installed_unit="$prefix/etc/containers/systemd/hololive-bot/$service.container"

python3 "$pair_tool" verify "$service" "$previous"
python3 "$pair_tool" check "$service" "$previous"
# pair는 그 image가 돌던 기본 unit(unit.container, SHA256SUMS로 검증)을 함께 보존한다. sync-opt-current.sh가 이미
# 새 revision의 기본 unit을 설치했으면, 이전 image를 새 unit 정의(env·HealthCmd·mount)로 띄우지 않도록 함께 되돌린다.
restore_unit=false
if [[ -f "$previous/unit.container" ]] && ! cmp -s -- "$previous/unit.container" "$installed_unit"; then
  restore_unit=true
elif [[ ! -f "$previous/unit.container" ]]; then
  echo "[WARN] $previous has no recorded base unit; the installed unit definition stays as is" >&2
fi
if [[ "$mode" == --check ]]; then
  [[ "$restore_unit" != true ]] || echo "[CHECK] --apply also restores the base unit the previous image ran with: $installed_unit"
  echo "[CHECK] $service previous pair verified: $previous"
  exit 0
fi

# fence 중에는 restart의 start가 condition으로 건너뛰어 writer가 멈춘 채 남는다.
for marker in fence.intent fenced; do
  [[ ! -e "$fence_dir/$marker" ]] || die "primary fence marker present: $fence_dir/$marker"
done
if [[ "$restore_unit" == true ]]; then
  # 바뀐 현재 unit은 release에 남겨 원인 확인과 재적용에 쓴다. 이름 변경 하나로 교체해 반쯤 쓴 unit이 남지 않는다.
  cp -p -- "$installed_unit" "$release/rollback-replaced-$service.container"
  unit_tmp="$(mktemp "$(dirname -- "$installed_unit")/.$service.container.XXXXXX")"
  install -m 0644 -- "$previous/unit.container" "$unit_tmp"
  mv -f -- "$unit_tmp" "$installed_unit"
  echo "[ROLLBACK] restored the base unit the previous image ran with; run sync-opt-current.sh for the intended revision before the next deployment"
fi
python3 "$pair_tool" activate "$service" "$previous" "$deploy_root"
systemctl daemon-reload
# Notify=healthy라 restart는 이전 image가 healthy가 된 뒤에만 성공한다.
systemctl restart "$unit"
systemctl is-active --quiet "$unit" || die "$unit is not active after restart"
python3 "$pair_tool" verify-live "$service" "$previous" "$service"
# 명시적 restart는 NRestarts를 0으로 되돌린다(systemd 257 실측). 0이 아니면 그 사이 자동 재시작이 있었다.
restarts="$(systemctl show "$unit" -p NRestarts --value)"
[[ "$restarts" == 0 ]] || die "$unit restarted automatically after rollback (NRestarts=$restarts)"
echo "[ROLLBACK] $service restored from $previous"
