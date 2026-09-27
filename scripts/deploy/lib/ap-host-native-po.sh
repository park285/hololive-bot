# shellcheck shell=bash
# Sourced in the bounded native deploy and rollback remote scripts.
po_service=hololive-youtube-po.service
po_socket=hololive-youtube-po.socket
po_unit_file=/etc/systemd/system/hololive-youtube-po.service
po_socket_file=/etc/systemd/system/hololive-youtube-po.socket

po_validate_release() {
  local release="$1" revision arch version
  revision="$(cat "$release/po-sandbox/revision")"
  arch="$(cat "$release/po-sandbox/architecture")"
  version="$(cat "$release/po-sandbox/version")"
  [[ "$revision" =~ ^[0-9a-f]{40}$ && "$arch" == amd64 && "$version" =~ ^[A-Za-z0-9._-]+$ ]] || return 1
  (cd "$release/po-sandbox" && sha256sum --check --strict rootfs.tar.sha256)
  sudo -n python3 "$release/bin/po-sandbox-manifest.py" verify \
    "$release/po-sandbox/rootfs" "$release/po-sandbox/rootfs-manifest.json" "$revision" "$arch"
  sudo -n "$release/po-sandbox/rootfs/app/bin/po-broker" --version | \
    python3 -c 'import json,sys; obj=json.load(sys.stdin); assert (obj["revision"],obj["version"],obj["goos"],obj["goarch"]) == (sys.argv[1],sys.argv[2],"linux","amd64")' "$revision" "$version"
}

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
  local previous="${1:-}" contract state
  sudo -n systemctl disable --now "$po_service" "$po_socket" >/dev/null 2>&1 || true
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
      sudo -n -u hololive "$previous/po-sandbox/rootfs/app/bin/po-broker" --healthcheck --socket /run/hololive-youtube-po/worker.sock
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
  (cd "$release/po-sandbox" && sha256sum --check --strict rootfs.tar.sha256)
  po_validate_release "$release"
  sudo -n install -m 0644 -o root -g root "$release/hololive-youtube-po.service" "$po_unit_file"
  sudo -n install -m 0644 -o root -g root "$release/hololive-youtube-po.socket" "$po_socket_file"
  sudo -n systemd-analyze verify "$po_unit_file" "$po_socket_file"
  sudo -n systemctl daemon-reload
  sudo -n systemctl enable --now "$po_socket"
  sudo -n systemctl restart "$po_service"
  sudo -n -u hololive "$release/po-sandbox/rootfs/app/bin/po-broker" --healthcheck --socket /run/hololive-youtube-po/worker.sock
}
