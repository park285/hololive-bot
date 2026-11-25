# shellcheck shell=bash
po_service="${slot_issuer_unit:?collector slot preamble required}"
po_socket="${po_service%.service}.socket"
po_unit_file="/etc/systemd/system/$po_service"
po_socket_file="/etc/systemd/system/$po_socket"

po_health_ready() {
  sudo -n -u hololive "$1/po-sandbox/rootfs/app/bin/po-broker" --healthcheck --socket /run/hololive-youtube-po/worker.sock
}

po_wait_ready() {
  local release="$1" _
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
  systemctl is-active --quiet "$po_socket" || return "$?"
  po_issuer_active || return "$?"
  [[ "$(systemctl show "$po_service" -p Result --value)" == success ]] || return 1
  [[ "$(systemctl show "$po_service" -p ExecMainStatus --value)" == 0 ]] || return 1
  journal="$(journalctl -u "$po_service" --since "$since" --no-pager)" || return "$?"
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
  python3 - "$service_file" "$working/$po_service" "$release" <<'PY' || exit
from pathlib import Path
import sys
source, target, release = sys.argv[1:]
text = Path(source).read_text()
command = "ExecStart=/app/bin/po-broker "
if text.count(command) != 1:
    raise SystemExit("unexpected issuer command; refusing unverifiable unit")
Path(target).write_text(text.replace(command, f"ExecStart={release}/po-sandbox/rootfs/app/bin/po-broker ", 1))
PY
  cp "$socket_file" "$working/$po_socket" || exit
  sudo -n systemd-analyze verify "$working/$po_service" "$working/$po_socket"
)

po_snapshot_previous() {
  local previous="$1" contract="$1/rollback-contract" state
  if sudo -n test -e "$po_unit_file" || sudo -n test -e "$po_socket_file"; then
    sudo -n test -r "$po_unit_file" && sudo -n test -r "$po_socket_file" || return 1
    sudo -n test -d "$previous/po-sandbox/rootfs" || return 1
    state=present
    sudo -n install -m 0644 -o root -g root "$po_unit_file" "$contract/$po_service"
    sudo -n install -m 0644 -o root -g root "$po_socket_file" "$contract/$po_socket"
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
      sudo -n install -m 0644 -o root -g root "$contract/$po_service" "$po_unit_file"
      sudo -n install -m 0644 -o root -g root "$contract/$po_socket" "$po_socket_file"
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
