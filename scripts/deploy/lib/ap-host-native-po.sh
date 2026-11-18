# shellcheck shell=bash
# 시간 상한이 있는 native 배포·롤백 원격 스크립트에서 불러온다.
po_service=hololive-youtube-po.service
po_socket=hololive-youtube-po.socket
po_unit_file=/etc/systemd/system/hololive-youtube-po.service
po_socket_file=/etc/systemd/system/hololive-youtube-po.socket

po_health_ready() {
  sudo -n -u hololive "$1/po-sandbox/rootfs/app/bin/po-broker" --healthcheck --socket /run/hololive-youtube-po/worker.sock
}

po_wait_ready() {
  local release="$1" _
  # health는 SDK import가 끝난 worker를 보장한다. 이 조회는 발급하지 않으며
  # native의 90초 준비 한도를 포함하도록 45회/2초 간격으로만 관찰한다.
  for _ in {1..45}; do
    if systemctl is-active --quiet "$po_service" && po_health_ready "$release"; then
      return 0
    fi
    sleep 2
  done
  echo 'issuer worker did not become ready within the startup bound' >&2
  return 1
}

po_issuer_active() {
  local attempt state
  for attempt in {1..6}; do
    if systemctl is-active --quiet "$po_service"; then return 0; fi
    state="$(systemctl show "$po_service" -p SubState --value)" || return 1
    case "$state" in
      auto-restart|auto-restart-queued|stop-post|start-pre|start) ;;
      dead) [[ -n "$(systemctl show "$po_service" -p Job --value)" ]] || return 1 ;;
      *) return 1 ;;
    esac
    (( attempt == 6 )) || sleep 1
  done
  return 1
}

po_require_running_without_failure() {
  local since="$1" journal result=0
  systemctl is-active --quiet "$po_socket" || return
  po_issuer_active || return
  # 정상 generation 퇴역도 service를 재기동하므로 issuer는 NRestarts 대신 종료 결과를 본다.
  [[ "$(systemctl show "$po_service" -p Result --value)" == success ]] || return 1
  [[ "$(systemctl show "$po_service" -p ExecMainStatus --value)" == 0 ]] || return 1
  journal="$(journalctl -u "$po_service" --since "$since" --no-pager)" || return
  grep -F 'Failed with result' <<<"$journal" >/dev/null || result=$?
  (( result == 1 ))
}

po_validate_release() {
  local release="$1" revision arch version
  revision="$(cat "$release/po-sandbox/revision")"
  arch="$(cat "$release/po-sandbox/architecture")"
  version="$(cat "$release/po-sandbox/version")"
  [[ "$revision" =~ ^[0-9a-f]{40}$ && "$arch" == amd64 && "$version" =~ ^[A-Za-z0-9._-]+$ ]] || return 1
  (cd "$release/po-sandbox" && sudo -n sha256sum --check --strict rootfs.tar.sha256)
  sudo -n python3 "$release/bin/po-sandbox-manifest.py" verify \
    "$release/po-sandbox/rootfs" "$release/po-sandbox/rootfs-manifest.json" "$revision" "$arch"
  sudo -n -u hololive "$release/po-sandbox/rootfs/app/bin/po-broker" --version | \
    python3 -c 'import json,sys; obj=json.load(sys.stdin); assert (obj["revision"],obj["version"],obj["goos"],obj["goarch"]) == (sys.argv[1],sys.argv[2],"linux","amd64")' "$revision" "$version"
}

po_verify_units() (
  set -e
  local release="$1" service_file="$2" socket_file="$3" working
  working="$(mktemp -d)" || exit
  trap 'rm -rf "$working"' EXIT
  # systemd-analyze는 RootDirectory를 무시하고 host에서 ExecStart를 검사한다.
  # 설치 unit은 보존하고 검사용 복사본의 실행 경로만 실제 rootfs로 해석한다.
  python3 - "$service_file" "$working/hololive-youtube-po.service" "$release" <<'PY' || exit
from pathlib import Path
import sys
source, target, release = sys.argv[1:]
text = Path(source).read_text()
command = "ExecStart=/app/bin/po-broker "
if text.count(command) != 1:
    raise SystemExit("unexpected issuer command; refusing unverifiable unit")
Path(target).write_text(text.replace(command, f"ExecStart={release}/po-sandbox/rootfs/app/bin/po-broker ", 1))
PY
  cp "$socket_file" "$working/hololive-youtube-po.socket" || exit
  sudo -n systemd-analyze verify "$working/hololive-youtube-po.service" "$working/hololive-youtube-po.socket"
)

po_snapshot_previous() {
  local previous="$1" contract="$1/rollback-contract" state
  if sudo -n test -e "$po_unit_file" || sudo -n test -e "$po_socket_file"; then
    sudo -n test -r "$po_unit_file" && sudo -n test -r "$po_socket_file" || return 1
    sudo -n test -d "$previous/po-sandbox/rootfs" || return 1
    state=present
    sudo -n install -m 0644 -o root -g root "$po_unit_file" "$contract/hololive-youtube-po.service"
    sudo -n install -m 0644 -o root -g root "$po_socket_file" "$contract/hololive-youtube-po.socket"
    po_validate_release "$previous"
  else
    state=absent
  fi
  printf '%s\n' "$state" | sudo -n tee "$contract/po-unit-presence" >/dev/null
  sudo -n chmod 0644 "$contract/po-unit-presence"
}

po_restore_previous() {
  local previous="${1:-}" contract state stop_status=0
  sudo -n systemctl disable --now "$po_service" "$po_socket" >/dev/null 2>&1 || stop_status=$?
  # 없는 unit의 기존 정리 계약은 유지하되 중단된 systemctl을 완료로 간주하지 않습니다.
  (( stop_status < 128 )) || return "$stop_status"
  if [[ -z "$previous" ]]; then
    state=absent
  else
    contract="$previous/rollback-contract"
    state="$(sudo -n cat "$contract/po-unit-presence")"
  fi
  case "$state" in
    present)
      po_validate_release "$previous"
      sudo -n install -m 0644 -o root -g root "$contract/hololive-youtube-po.service" "$po_unit_file"
      sudo -n install -m 0644 -o root -g root "$contract/hololive-youtube-po.socket" "$po_socket_file"
      sudo -n systemctl daemon-reload
      sudo -n systemctl enable --now "$po_socket"
      sudo -n systemctl start "$po_service"
      po_wait_ready "$previous"
      ;;
    absent)
      sudo -n rm -f "$po_unit_file" "$po_socket_file"
      sudo -n systemctl daemon-reload
      if sudo -n systemctl is-active --quiet "$po_service" || sudo -n systemctl is-active --quiet "$po_socket"; then
        echo 'issuer units must be absent after restoration' >&2
        return 1
      fi
      ;;
    *) echo 'invalid previous issuer unit state' >&2; return 1 ;;
  esac
}

po_install_release() {
  local release="$1" revision
  revision="$(cat "$release/po-sandbox/revision")"
  [[ "$revision" == "$2" ]] || return 1
  (cd "$release/po-sandbox" && sudo -n sha256sum --check --strict rootfs.tar.sha256)
  po_validate_release "$release"
  sudo -n install -m 0644 -o root -g root "$release/hololive-youtube-po.service" "$po_unit_file"
  sudo -n install -m 0644 -o root -g root "$release/hololive-youtube-po.socket" "$po_socket_file"
  po_verify_units "$release" "$po_unit_file" "$po_socket_file"
  sudo -n systemctl daemon-reload
  if [[ "${window_action:-deploy}" == resume ]]; then
    native_window_admit_candidate
    sudo -n systemctl enable "$po_socket"
    native_window_start "$po_socket"
    native_window_start "$po_service"
    native_window_open_rotation
  elif [[ "${same_window:-false}" == true ]]; then
    native_apply_start_issuer
  else
    sudo -n systemctl enable --now "$po_socket"
    sudo -n systemctl restart "$po_service"
  fi
  po_wait_ready "$release"
  if [[ "${window_action:-deploy}" == resume || "${same_window:-false}" == true ]]; then native_window_candidates "$po_socket" "$po_service"; fi
}
