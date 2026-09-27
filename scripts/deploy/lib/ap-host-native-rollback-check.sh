#!/usr/bin/env bash

native_rollback_validate() {
  local previous_target="$1"
  local contract_dir="$previous_target/rollback-contract"
  local rel

  if [[ -z "$previous_target" || ! -d "$previous_target" ]]; then
    echo "previous host-native release is unavailable; refusing partial rollback" >&2
    return 1
  fi
  for rel in bin/youtube-collector bin/youtube-collector-wrapper bin/healthcheck; do
    if ! sudo -n test -x "$previous_target/$rel"; then
      echo "previous host-native executable is missing or not executable: $rel" >&2
      return 1
    fi
  done
  if ! sudo -n test -f "$previous_target/youtubejs/src/server.mjs"; then
    echo "previous host-native youtubejs helper is missing" >&2
    return 1
  fi
  for rel in youtube-collector-host.env hololive-youtube-collector@.service SHA256SUMS previous-before-cutover; do
    if ! sudo -n test -r "$contract_dir/$rel"; then
      echo "previous host-native rollback contract is incomplete: $rel" >&2
      return 1
    fi
  done
  if ! sudo -n test -r "$contract_dir/po-unit-presence"; then
    echo 'previous issuer unit presence snapshot is missing' >&2
    return 1
  fi
  local earlier_previous
  earlier_previous="$(sudo -n cat "$contract_dir/previous-before-cutover")"
  if [[ "$earlier_previous" != absent ]]; then
    [[ "$earlier_previous" =~ ^/opt/hololive-bot/youtube-collector/releases/[A-Za-z0-9._-]+$ ]] || return 1
    sudo -n test -d "$earlier_previous" && sudo -n test ! -L "$earlier_previous" || return 1
  fi

  if ! sudo -n sh -n "$previous_target/bin/youtube-collector-wrapper"; then
    echo "previous host-native wrapper failed syntax validation" >&2
    return 1
  fi
  if ! sudo -n sh -c 'cd "$1" && sha256sum --check --strict rollback-contract/SHA256SUMS >/dev/null' sh "$previous_target"; then
    echo "previous host-native rollback payload failed checksum validation" >&2
    return 1
  fi
  if ! sudo -n systemd-analyze verify "$contract_dir/hololive-youtube-collector@.service"; then
    echo "previous host-native systemd unit failed validation" >&2
    return 1
  fi
  local po_state
  po_state="$(sudo -n cat "$contract_dir/po-unit-presence")"
  case "$po_state" in
    present)
      sudo -n test -r "$contract_dir/hololive-youtube-po.service"
      sudo -n test -r "$contract_dir/hololive-youtube-po.socket"
      po_validate_release "$previous_target"
      sudo -n systemd-analyze verify "$contract_dir/hololive-youtube-po.service" "$contract_dir/hololive-youtube-po.socket"
      ;;
    absent)
      sudo -n test ! -e "$contract_dir/hololive-youtube-po.service"
      sudo -n test ! -e "$contract_dir/hololive-youtube-po.socket"
      ;;
    *) echo 'invalid previous issuer unit presence state' >&2; return 1 ;;
  esac
}
