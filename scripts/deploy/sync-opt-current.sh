#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="${REPO_ROOT:-$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)}"
OPT_ROOT="${OPT_ROOT:-/opt/hololive-bot/compose}"
OPT_CURRENT="${OPT_ROOT}/current"
SBIN_DIR="${SBIN_DIR:-/usr/local/sbin}"
DROPIN_DIR="${DROPIN_DIR:-/etc/systemd/system/hololive-compose.service.d}"
UNIT="hololive-compose.service"

log() { printf '[sync-opt-current] %s\n' "$*"; }
die() { printf '[sync-opt-current] ERROR: %s\n' "$*" >&2; exit 1; }

[[ "$(id -u)" -eq 0 ]] || die "must run as root (sudo)"

assert_clean() {
  local dir="$1" name="$2"
  git -C "$dir" rev-parse --git-dir >/dev/null 2>&1 || die "$name git tree missing: $dir"
  # HEAD 만 배포(git archive)하므로 tracked 변경만 SSOT 위반으로 본다. 서브모듈의 AGENTS.md/
  # CLAUDE.md 같은 의도된 untracked 로컬 파일은 clean 판정에서 제외한다.
  if [[ -n "$(git -C "$dir" status --porcelain --untracked-files=no)" ]]; then
    git -C "$dir" status --short --untracked-files=no >&2
    die "$name has uncommitted tracked changes; commit/stash before sync"
  fi
}

assert_clean "$REPO_ROOT" "repo"
HEAD_SHA="$(git -C "$REPO_ROOT" rev-parse --short HEAD)"
log "repo=$REPO_ROOT HEAD=$HEAD_SHA -> $OPT_CURRENT"

if [[ -d "$OPT_CURRENT" ]]; then
  BACKUP="${OPT_CURRENT}.bak-${HEAD_SHA}"
  rm -rf "$BACKUP"
  cp -a "$OPT_CURRENT" "$BACKUP"
  log "backup -> $BACKUP"
fi

# staging 은 OPT_ROOT 와 같은 파일시스템에 둔다: cross-device rsync(/tmp 별도 mount) 회피.
STAGING="$(mktemp -d "${OPT_ROOT}/.staging-current.XXXXXX")"
cleanup() { rm -rf "$STAGING"; }
trap cleanup EXIT
git -C "$REPO_ROOT" archive HEAD | tar -x -C "$STAGING"
log "staged tracked tree ($(find "$STAGING" -type f | wc -l) files)"

for sib in shared-go iris-client-go; do
  src="${REPO_ROOT}/../${sib}"
  assert_clean "$src" "sibling $sib"
  sib_staging="$(mktemp -d "${OPT_ROOT}/.staging-${sib}.XXXXXX")"
  git -C "$src" archive HEAD | tar -x -C "$sib_staging"
  rsync -a --delete "$sib_staging/" "${OPT_ROOT}/${sib}/"
  rm -rf "$sib_staging"
  chown -R root:root "${OPT_ROOT}/${sib}"
  log "sibling synced: ${sib} ($(git -C "$src" rev-parse --short HEAD))"
done

# current 반영: tracked 트리만 교체하고 런타임 bind-mount 데이터는 보존한다. staging 에는
# data/logs/backups 가 없으므로 --delete 가 OPT_CURRENT 의 기존 런타임 데이터를 지우지 않도록
# 명시적으로 제외한다.
mkdir -p "$OPT_CURRENT"
rsync -a --delete \
  --exclude=/data --exclude=/logs --exclude=/backups --exclude=/runtime-config \
  "$STAGING/" "$OPT_CURRENT/"

# runtime-config 는 tracked 템플릿과 gitignored 런타임 설정(iris_base_url, iris-ca.pem)이
# 섞여 있다. git archive 는 후자를 빠뜨려 alarm-worker 의 IRIS_BASE_URL_FILE 로딩이 깨지므로,
# data 와 함께 dev tree(LIVE) 전체를 보존 이관한다.
for d in data runtime-config; do
  if [[ -d "$REPO_ROOT/$d" ]]; then
    mkdir -p "$OPT_CURRENT/$d"
    rsync -a "$REPO_ROOT/$d/" "$OPT_CURRENT/$d/"
  fi
done
mkdir -p "$OPT_CURRENT/logs" "$OPT_CURRENT/backups"
log "synced current (runtime data + runtime-config preserved)"

# 권한: root systemd 가 실행하는 exec_tree 와 그 부모 chain 은 root-owned·non-writable 여야
# verify-exec-tree-ownership 를 통과한다(03e6dca8/4d57f81c). 런타임 데이터만 컨테이너 uid 1000.
chown -R root:root "$OPT_CURRENT"
chmod -R go-w "$OPT_CURRENT"
for d in data logs backups; do
  [[ -d "$OPT_CURRENT/$d" ]] && chown -R 1000:1000 "$OPT_CURRENT/$d"
done
chown root:root "$OPT_ROOT" "$(dirname "$OPT_ROOT")"
log "ownership normalized (exec tree root, runtime data uid 1000)"

install -m0755 -o root -g root "$REPO_ROOT/scripts/deploy/systemd-compose-up.sh" "$SBIN_DIR/hololive-compose-up"
install -m0755 -o root -g root "$REPO_ROOT/scripts/deploy/systemd-compose-down.sh" "$SBIN_DIR/hololive-compose-down"
log "wrappers installed (opt-in live-compat + verifier self-check)"

mkdir -p "$DROPIN_DIR"
install -m0644 -o root -g root "$REPO_ROOT"/scripts/systemd/hololive-compose.service.d/*.conf "$DROPIN_DIR/"
log "drop-ins installed -> $DROPIN_DIR"

systemctl daemon-reload
log "daemon-reload done"

verifier="$OPT_CURRENT/scripts/deploy/verify-exec-tree-ownership.sh"
exec_tree=(
  "$verifier"
  "$OPT_CURRENT/scripts/deploy/systemd-compose-up.sh"
  "$OPT_CURRENT/scripts/deploy/systemd-compose-down.sh"
  "$OPT_CURRENT/scripts/deploy/compose.sh"
  "$OPT_CURRENT/scripts/deploy/lib/compose-env.sh"
  "$OPT_CURRENT/scripts/deploy/lib/removed-runtimes.sh"
)
while IFS= read -r yml; do
  exec_tree+=("$yml")
done < <(find "$OPT_CURRENT/deploy/compose" -maxdepth 1 -type f -name 'docker-compose*.yml' | sort)
bash "$verifier" "${exec_tree[@]}" || die "exec-tree ownership verification failed"
log "exec-tree ownership verified"

if ! systemctl show -p Environment "$UNIT" | grep -q 'HOLOLIVE_ENABLE_LIVE_COMPAT=1'; then
  die "drop-in did not surface HOLOLIVE_ENABLE_LIVE_COMPAT=1 in merged unit Environment"
fi
log "live-compat drop-in asserted; SSOT sync complete (HEAD=$HEAD_SHA)"
log "next: sudo systemctl restart $UNIT  (recreates stack from /opt current with live-compat)"
