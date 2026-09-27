#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
deploy_script="$ROOT_DIR/scripts/deploy/ap-deploy.sh"
source_snapshot="$ROOT_DIR/scripts/deploy/lib/ap-source-snapshot.py"
fail() { echo "[FAIL] $*" >&2; exit 1; }
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
mkdir -p "$tmp/bin"

# 실제 실패 처리 함수와 source snapshot을 실행한다. Docker만 격리된 상태 저장소로 대체한다.
handler_body="$(sed -n '/^restore_after_failed_deploy() {$/,/^}$/p' "$deploy_script")"
stop_body="$(sed -n '/^stop_failed_first_deploy() {$/,/^}$/p' "$deploy_script")"
cat > "$tmp/bin/docker" <<'DOCKER'
#!/usr/bin/env bash
set -euo pipefail
case "$1" in
  ps)
    [[ "${FAIL_PS:-false}" != true ]] || exit 31
    name="${@: -1}"
    name="${name#name=^}"
    name="${name%\$}"
    file="$STATE/containers/$name"
    if [[ -f "$file" ]] && { [[ "$2" == *a* ]] || [[ "$(<"$file")" == running ]]; }; then
      printf '%s\n' "$name"
    fi
    ;;
  stop)
    [[ -f "$STATE/containers/$2" ]] || exit 32
    if [[ "${KEEP_RUNNING:-false}" != true ]]; then printf 'exited\n' > "$STATE/containers/$2"; fi
    ;;
  rm)
    [[ "$(<"$STATE/containers/$2")" != running ]] || exit 33
    rm "$STATE/containers/$2"
    ;;
  image)
    case "$2" in
      ls)
        reference="${@: -1}"
        reference="${reference#reference=}"
        name="${reference//[:\/]/_}"
        [[ ! -f "$STATE/images/$name" ]] || printf '%s\n' "$name"
        ;;
      rm)
        [[ "${FAIL_IMAGE_REMOVE:-false}" != true ]] || exit 34
        name="${3//[:\/]/_}"
        rm "$STATE/images/$name"
        ;;
      *) exit 35 ;;
    esac
    ;;
  *) exit 36 ;;
esac
DOCKER
cat > "$tmp/bin/sudo" <<'SUDO'
#!/usr/bin/env bash
set -euo pipefail
[[ "$1" == -n ]] && shift
exec "$@"
SUDO
chmod +x "$tmp/bin/docker" "$tmp/bin/sudo"

prepare() {
  case_root="$tmp/$1"
  mkdir -p "$case_root/home/hololive-bot/backups/first" "$case_root/state/containers" "$case_root/state/images"
  printf 'original\n' > "$case_root/home/hololive-bot/source-marker"
  printf 'hololive-bot/source-marker\n' > "$case_root/manifest"
  python3 "$source_snapshot" capture "$case_root/home" "$case_root/home/hololive-bot/backups/first" "$case_root/manifest" >/dev/null
  printf 'candidate\n' > "$case_root/home/hololive-bot/source-marker"
  printf 'running\n' > "$case_root/state/containers/unrelated"
  touch "$case_root/state/images/unrelated"
}
load_candidates() {
  printf 'running\n' > "$case_root/state/containers/hololive-youtube-collector-b"
  printf 'running\n' > "$case_root/state/containers/hololive-youtube-po-b"
  touch "$case_root/state/images/hololive-youtube-collector_prod" "$case_root/state/images/hololive-youtube-po-sandbox_prod"
  printf 'candidate\n' > "$case_root/home/hololive-bot/backups/po-sandbox-current-b.json"
  printf 'candidate\n' > "$case_root/home/hololive-bot/backups/po-sandbox-current-b.image-id"
}
# shellcheck disable=SC2034,SC2329 # 실제 소스에서 읽은 함수의 변수·외부 경계다.
run_failure() (
  export HOME="$case_root/home" STATE="$case_root/state" PATH="$tmp/bin:$PATH"
  remote() { bash -c "$1"; }
  fake_ssh() { bash -c "$1"; }
  eval "$handler_body"
  eval "$stop_body"
  AP_NAME=seoul
  AP_SSH=(fake_ssh)
  REMOTE_REPO_DIR=hololive-bot
  backup_dir=backups/first
  containers_list=hololive-youtube-collector-b
  IMAGE_REF=hololive-youtube-collector:prod
  PO_IMAGE_REF=hololive-youtube-po-sandbox:prod
  po_manifest_active=backups/po-sandbox-current-b.json
  po_image_id_active=backups/po-sandbox-current-b.image-id
  cutover_armed=true
  source_armed=true
  rollback_available=false
  restore_after_failed_deploy 7
)
assert_recoverable() {
  [[ ! -e "$case_root/state/containers/hololive-youtube-collector-b" && ! -e "$case_root/state/containers/hololive-youtube-po-b" ]] || fail 'failed candidate containers prevent the next first deploy'
  [[ ! -e "$case_root/state/images/hololive-youtube-collector_prod" && ! -e "$case_root/state/images/hololive-youtube-po-sandbox_prod" ]] || fail 'failed candidate images prevent the next first deploy'
  [[ ! -e "$case_root/home/hololive-bot/backups/po-sandbox-current-b.json" && ! -e "$case_root/home/hololive-bot/backups/po-sandbox-current-b.image-id" ]] || fail 'active issuer receipts prevent the next first deploy'
  [[ "$(<"$case_root/home/hololive-bot/source-marker")" == original ]] || fail 'source snapshot was not restored'
  [[ "$(<"$case_root/state/containers/unrelated")" == running && -f "$case_root/state/images/unrelated" ]] || fail 'unrelated runtime changed'
  [[ -f "$case_root/home/hololive-bot/backups/first/source-prechange.tar" ]] || fail 'recovery archive was removed'
}

prepare loaded
load_candidates
rc=0
run_failure > "$case_root/output" 2>&1 || rc="$?"
[[ "$rc" == 7 ]] || fail 'original deploy failure status was lost'
assert_recoverable

prepare before_load
rc=0
run_failure > "$case_root/output" 2>&1 || rc="$?"
[[ "$rc" == 7 ]] || fail 'pre-load failure status was lost'
assert_recoverable

prepare still_running
load_candidates
rc=0
KEEP_RUNNING=true run_failure > "$case_root/output" 2>&1 || rc="$?"
[[ "$rc" == 7 ]] || fail 'stop failure replaced the original deploy failure'
[[ "$(<"$case_root/state/containers/hololive-youtube-collector-b")" == running ]] || fail 'fixture did not retain the active container'
[[ -f "$case_root/state/images/hololive-youtube-collector_prod" ]] || fail 'image was removed while its container remained active'
[[ "$(<"$case_root/home/hololive-bot/source-marker")" == candidate ]] || fail 'source restored while the failed candidate remained active'

prepare image_remove_failure
load_candidates
rc=0
FAIL_IMAGE_REMOVE=true run_failure > "$case_root/output" 2>&1 || rc="$?"
[[ "$rc" == 7 ]] || fail 'image cleanup failure replaced the original deploy failure'
[[ -f "$case_root/state/images/hololive-youtube-collector_prod" ]] || fail 'fixture did not retain the image'
[[ "$(<"$case_root/home/hololive-bot/source-marker")" == candidate ]] || fail 'source restored after incomplete cleanup'

echo 'AP first-deploy recovery state checks passed'
