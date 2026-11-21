#!/usr/bin/env bash

native_profile_pair_validate() {
  local release="$1" env_file="$2"
  sudo -n test -f "$release/worker-profile.json" || {
    echo 'native release has no paired profile; approved --prepare-profile must precede mirror changes or maintenance stop' >&2
    return 1
  }
  sudo -n test ! -L "$release/worker-profile.json" || return 1
  sudo -n grep -Fxq "STACK_WORKER_PROFILE_FILE=$release/worker-profile.json" "$env_file" || return 1
  sudo -n sh -c 'cd "$1" && sha256sum --check --strict SHA256SUMS >/dev/null' sh "$release" || return 1
  sudo -n systemd-run --quiet --wait --pipe --collect \
    --property=User=hololive --property=Group=opc --property=PrivateNetwork=yes \
    --property=NoNewPrivileges=yes --property=RuntimeMaxSec=30s \
    --property="WorkingDirectory=$release" --property=ProtectHome=yes --property=ProtectSystem=strict \
    --property=PrivateTmp=yes \
    --property=EnvironmentFile=/etc/stack-secrets/hololive-bot/collector.env \
    --property="EnvironmentFile=$env_file" \
    /usr/bin/env "STACK_WORKER_PROFILE_FILE=$release/worker-profile.json" \
    "$release/bin/hololive-collector-wrapper" "$release/bin/hololive-collector" --check-worker-profile
}

native_root_endpoint() {
  sudo -n bash -c "$(declare -f compose_env_validate_file_format compose_env_read_value_from_file postgres_endpoint_validate postgres_endpoint_env); postgres_endpoint_env \"\$1\"" _ "$1"
}

native_rollback_endpoint_validate() {
  local previous_target="$1" current_env="$2" previous_endpoint current_endpoint
  : "${slot_host_env?}"
  sudo -n test ! -e "$previous_target/native-window" || {
    echo '정지 창이 퇴역시킨 native release는 endpoint가 같아도 복원할 수 없습니다.' >&2
    return 1
  }
  previous_endpoint="$(native_root_endpoint "$previous_target/rollback-contract/${slot_host_env##*/}")" || return
  current_endpoint="$(native_root_endpoint "$current_env")" || return
  postgres_endpoint_require_same "$current_endpoint" "$previous_endpoint"
}

native_rollback_validate() {
  : "${slot_host_env?}" "${slot_collector_unit?}" "${slot_root?}" "${po_service?}" "${po_socket?}"
  local previous_target="$1"
  local contract_dir="$previous_target/rollback-contract"
  local rel

  if [[ -z "$previous_target" || ! -d "$previous_target" ]]; then
    echo "previous host-native release is unavailable; refusing partial rollback" >&2
    return 1
  fi
  for rel in bin/hololive-collector bin/hololive-collector-wrapper bin/healthcheck; do
    if ! sudo -n test -x "$previous_target/$rel"; then
      echo "previous host-native executable is missing or not executable: $rel" >&2
      return 1
    fi
  done
  if ! sudo -n test -f "$previous_target/youtubejs/src/server.mjs"; then
    echo "previous host-native youtubejs helper is missing" >&2
    return 1
  fi
  for rel in "${slot_host_env##*/}" "${slot_collector_unit%%@*}@.service" SHA256SUMS previous-before-cutover; do
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
    [[ "$earlier_previous" == "$slot_root/releases/"* && "${earlier_previous#"$slot_root/releases/"}" =~ ^[A-Za-z0-9._-]+$ ]] || return 1
    sudo -n test -d "$earlier_previous" && sudo -n test ! -L "$earlier_previous" || return 1
  fi

  if ! sudo -n sh -n "$previous_target/bin/hololive-collector-wrapper"; then
    echo "previous host-native wrapper failed syntax validation" >&2
    return 1
  fi
  if ! sudo -n sh -c 'cd "$1" && sha256sum --check --strict rollback-contract/SHA256SUMS >/dev/null' sh "$previous_target"; then
    echo "previous host-native rollback payload failed checksum validation" >&2
    return 1
  fi
  native_profile_pair_validate "$previous_target" "$contract_dir/${slot_host_env##*/}" || return 1
  if ! sudo -n systemd-analyze verify "$contract_dir/${slot_collector_unit%%@*}@.service"; then
    echo "previous host-native systemd unit failed validation" >&2
    return 1
  fi
  local po_state
  po_state="$(sudo -n cat "$contract_dir/po-unit-presence")"
  case "$po_state" in
    present)
      sudo -n test -r "$contract_dir/$po_service"
      sudo -n test -r "$contract_dir/$po_socket"
      po_validate_release "$previous_target"
      po_verify_units "$previous_target" "$contract_dir/$po_service" "$contract_dir/$po_socket"
      ;;
    absent)
      sudo -n test ! -e "$contract_dir/$po_service"
      sudo -n test ! -e "$contract_dir/$po_socket"
      ;;
    *) echo 'invalid previous issuer unit presence state' >&2; return 1 ;;
  esac
}
