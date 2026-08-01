#!/usr/bin/env bash

native_rollback_validate() {
  local previous_target="$1"
  local contract_dir="$previous_target/rollback-contract"
  local rel

  if [[ -z "$previous_target" || ! -d "$previous_target" ]]; then
    echo "previous host-native release is unavailable; refusing partial rollback" >&2
    return 1
  fi
  for rel in bin/youtube-producer bin/youtube-producer-wrapper bin/healthcheck; do
    if ! sudo -n test -x "$previous_target/$rel"; then
      echo "previous host-native executable is missing or not executable: $rel" >&2
      return 1
    fi
  done
  if ! sudo -n test -d "$previous_target/internal/domain/data" ||
     ! sudo -n find "$previous_target/internal/domain/data" -type f -print -quit | grep -q .; then
    echo "previous host-native runtime data is missing or empty" >&2
    return 1
  fi
  for rel in youtube-producer-host.env hololive-youtube-producer@.service SHA256SUMS; do
    if ! sudo -n test -r "$contract_dir/$rel"; then
      echo "previous host-native rollback contract is incomplete: $rel" >&2
      return 1
    fi
  done

  if ! sudo -n sh -n "$previous_target/bin/youtube-producer-wrapper"; then
    echo "previous host-native wrapper failed syntax validation" >&2
    return 1
  fi
  if ! sudo -n sh -c 'cd "$1" && sha256sum --check --strict rollback-contract/SHA256SUMS >/dev/null' sh "$previous_target"; then
    echo "previous host-native rollback payload failed checksum validation" >&2
    return 1
  fi
  if ! sudo -n systemd-analyze verify "$contract_dir/hololive-youtube-producer@.service"; then
    echo "previous host-native systemd unit failed validation" >&2
    return 1
  fi
}
