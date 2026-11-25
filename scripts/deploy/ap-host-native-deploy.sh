#!/usr/bin/env bash
set -euo pipefail
[[ "$(hostname -s)" == kapu ]] || { echo 'native issuer artifact builds are restricted to kapu' >&2; exit 1; }

tool_script="$(realpath -e -- "${BASH_SOURCE[0]}")"
tool_root="$(cd "${tool_script%/*}/../.." && pwd -P)"
REPO_ROOT="$(realpath -e -- "${REPO_ROOT:-$tool_root}")"
MODE="${2:---dry-run}"
AP_REQUIRED_UDP_BUFFER_BYTES="${AP_REQUIRED_UDP_BUFFER_BYTES:-7500000}"
AP_SWAPFILE_SIZE_MIB="${AP_SWAPFILE_SIZE_MIB:-2048}"
AP_POSTGRES_PORT="${AP_POSTGRES_PORT:-5433}"
if [[ "$AP_POSTGRES_PORT" != 5433 ]]; then
  echo "AP_POSTGRES_PORT must be the central PostgreSQL TLS port (5433)" >&2
  exit 2
fi
for checkout in "$tool_root" "$REPO_ROOT"; do
  [[ "$(git -C "$checkout" rev-parse --show-toplevel)" == "$checkout" ]] || {
    echo 'native tool and product paths must be checkout top-level directories' >&2
    exit 1
  }
done

. "$tool_root/scripts/deploy/lib/source-revision.sh"
tool_revision="$(deploy_source_revision "$tool_root")"
native_revision="$tool_revision"
if [[ "$REPO_ROOT" != "$tool_root" ]]; then native_revision="$(deploy_source_revision "$REPO_ROOT")"; fi
native_tool_blob() { git -C "$tool_root" cat-file blob "$tool_revision:$1"; }
native_product_blob() { git -C "$REPO_ROOT" cat-file blob "$native_revision:$1"; }

host_env_template() {
  awk '/^write_host_env\(\) \{$/ { body=1; count++ } body { print } /^}$/ { body=0 }
       END { if (body || count != 1) exit 1 }'
}

if [[ "$REPO_ROOT" != "$tool_root" || -v AP_NATIVE_TOOL_REVISION || -v AP_NATIVE_PRODUCT_REVISION ]]; then
  [[ "$tool_revision" == "${AP_NATIVE_TOOL_REVISION:-}" && "$native_revision" == "${AP_NATIVE_PRODUCT_REVISION:-}" ]] || {
    echo 'native tool/product checkout does not match the two approved full revisions' >&2
    exit 1
  }
  deploy_entry=scripts/deploy/ap-host-native-deploy.sh
  native_tool_blob "$deploy_entry" | cmp -s - "$tool_script" || {
    echo 'native deploy caller bytes do not match the approved tool' >&2
    exit 1
  }
  native_product_blob "$deploy_entry" | host_env_template | cmp -s - <(host_env_template < "$tool_script") || {
    echo 'host env template differs from the approved product source' >&2
    exit 1
  }
fi
printf 'native deploy source: tool=%s product=%s\n' "$tool_revision" "$native_revision" >&2


. "$tool_root/scripts/deploy/lib/ap-host-native-window.sh"
if [[ "$REPO_ROOT" != "$tool_root" ]]; then
  product_window="$(unset native_release_window; . "$REPO_ROOT/scripts/deploy/lib/ap-host-native-window.sh"; printf '%s' "${native_release_window:?}")"
  [[ "$product_window" == "$native_release_window" ]] || { echo 'native tool/product release window mismatch' >&2; exit 1; }
fi
AP_POSTGRES_HOST=""
while IFS='=' read -r host_name host_address; do
  if [[ "$host_name" == central ]]; then AP_POSTGRES_HOST="$host_address"; fi
done < "$REPO_ROOT/deploy/topology/hosts.conf"
[[ -n "$AP_POSTGRES_HOST" ]] || { echo 'topology central address missing' >&2; exit 1; }
case "$MODE" in
  --dry-run|--apply|--prepare-profile) [[ $# -le 2 ]] || exit 2 ;;
  *)
    echo "Usage: $0 <ap-host> [--dry-run|--apply|--prepare-profile]" >&2
    exit 2
    ;;
esac

. "$tool_root/scripts/deploy/lib/ap-host.sh"
. "$tool_root/scripts/deploy/lib/ap-host-native-release-path.sh"

CUTOVER_LIBS=(compose-env postgres-endpoint ap-host-native-release-path ap-host-native-po ap-host-native-cutover ap-host-native-window
  ap-host-native-rollback-check youtubejs-node-version ap-collector-readiness ap-host-native-remote-apply)
COLLECTOR_WRAPPER_LIB="$REPO_ROOT/scripts/deploy/lib/ap-host-native-collector-wrapper.sh"
UNIT_TEMPLATE="$REPO_ROOT/scripts/deploy/lib/hololive-collector.service"
PO_UNIT_TEMPLATE="$REPO_ROOT/scripts/deploy/lib/hololive-youtube-po.service"
PO_SOCKET_TEMPLATE="$REPO_ROOT/scripts/deploy/lib/hololive-youtube-po.socket"
ap_host_load "$REPO_ROOT" "${1:-}"
slot_collector_unit="${slot_collector_unit:?collector slot preamble required}" slot_host_env="${slot_host_env:?collector slot preamble required}" slot_issuer_unit="${slot_issuer_unit:?collector slot preamble required}" slot_root="${slot_root:?collector slot preamble required}" slot_service="${slot_service:?collector slot preamble required}"
. "$tool_root/scripts/deploy/lib/collector-slots.sh"
AP_SLOT_PREAMBLE="$(collector_slot_preamble "${AP_SLOTS[0]}" <(native_tool_blob scripts/deploy/lib/collector-slots.tsv))"
[[ "$AP_SLOT_PREAMBLE" == "$(collector_slot_preamble "${AP_SLOTS[0]}" "$REPO_ROOT/scripts/deploy/lib/collector-slots.tsv")" ]] ||
  { echo 'native tool/product collector slot table mismatch' >&2; exit 1; }
AP_POSTGRES_POOL_MAX_CONNS=""
while IFS='|' read -r owner _service _key _source _instances maximum _role; do
  if [[ "$owner" == "$slot_service" ]]; then AP_POSTGRES_POOL_MAX_CONNS="$maximum"; fi
done < "$REPO_ROOT/scripts/ci/postgres-capacity-policy.tsv"
[[ "$AP_POSTGRES_POOL_MAX_CONNS" =~ ^[1-9][0-9]*$ ]] || { echo 'collector capacity policy missing' >&2; exit 1; }

if [[ ! "$AP_REQUIRED_UDP_BUFFER_BYTES" =~ ^[0-9]+$ ]]; then
  echo "AP_REQUIRED_UDP_BUFFER_BYTES must be an integer" >&2
  exit 2
fi
if [[ ! "$AP_SWAPFILE_SIZE_MIB" =~ ^[1-9][0-9]*$ ]]; then
  echo "AP_SWAPFILE_SIZE_MIB must be a positive integer" >&2
  exit 2
fi

if [[ ${#AP_SERVICES[@]} -ne 1 || ${#AP_PORTS[@]} -ne 1 ]]; then
  echo "host-native deploy currently supports exactly one AP service per host" >&2
  exit 2
fi

if [[ "$MODE" != "--dry-run" && "${!AP_APPROVE_DEPLOY_VAR:-}" != "true" ]]; then
  echo "Refusing apply without $AP_APPROVE_DEPLOY_VAR=true" >&2
  exit 2
fi
if [[ "$MODE" == --prepare-profile ]]; then
  service="${AP_SERVICES[0]}"
  native_tool_blob scripts/deploy/lib/worker-profile-pair.py |
    "${AP_SSH[@]}" "sudo -n python3 - prepare-native '$service' '$slot_root/current' '$slot_host_env' '$slot_collector_unit'"
  exit 0
fi

release_id="${RELEASE_ID:-$(date -u +%Y%m%dT%H%M%SZ)-${native_revision:0:12}-$AP_NAME}"
artifact_dir="${ARTIFACT_DIR:-$REPO_ROOT/artifacts/ap-host-native/$release_id}"
native_release_id_validate "$release_id"

if [[ "$AP_GOAMD64" != v1 ]]; then
  if ! ap_remote_bash "$AP_GOAMD64" <<'REMOTE'
/lib64/ld-linux-x86-64.so.2 --help | grep -Fq "x86-64-$1 (supported"
REMOTE
  then
    echo "Refusing native AP deploy for $AP_NAME: host CPU does not report x86-64-$AP_GOAMD64 support" >&2
    exit 2
  fi
fi

service="${AP_SERVICES[0]}"
port="${AP_PORTS[0]}"
payload_name=".hololive-host-native-${AP_NAME}-${release_id}"
version="${HOLO_BOT_VERSION:-${native_revision:0:12}}"

write_host_env() {
  local dest="$1"
  {
    printf 'APP_ENV=production\n'
    printf 'NOTIFICATION_EGRESS_ROLE=off\n'
    printf 'COLLECTOR_RUNTIME_ALLOWED=true\n'
    printf 'COLLECTOR_INSTANCE_ID=%s\n' "$service"
    printf 'PGAPPNAME=%s\n' "$service"
    printf 'COLLECTOR_LOG_FILE_NAME=%s.log\n' "$service"
    printf 'STACK_WORKER_PROFILE_FILE=%s/releases/%s/worker-profile.json\n' "$slot_root" "$release_id"
    printf 'STACK_WORKER_PROFILE_SOURCE_FILE=/etc/stack-secrets/hololive-bot/worker-profiles/%s.json\n' "$service"
    printf 'SERVER_PORT=%s\n' "$port"
    printf 'HOLOLIVE_HTTP_TRANSPORTS=h3\n'
    printf 'HOLOLIVE_H3_ADDR=127.0.0.1:%s\n' "$port"
    printf 'HOLOLIVE_H3_CERT_FILE=/etc/stack-secrets/hololive-bot/certs/hololive-h3.crt\n'
    printf 'HOLOLIVE_H3_KEY_FILE=/etc/stack-secrets/hololive-bot/certs/hololive-h3.key\n'
    printf 'HOLOLIVE_METRICS_ADDR=%s:30096\n' "$AP_SSH_HOST"
    printf 'HEALTHCHECK_CA_CERT_FILE=/etc/stack-secrets/hololive-bot/certs/hololive-h3.crt\n'
    printf 'HEALTHCHECK_SERVER_NAME=127.0.0.1\n'
    printf 'YOUTUBEJS_NODE=/usr/bin/node\n'
    printf 'YOUTUBEJS_SCRIPT=%s/current/youtubejs/src/server.mjs\n' "$slot_root"
    printf 'COLLECTOR_YOUTUBEJS_STARTUP_TIMEOUT_SECONDS=90\n'
    printf 'POSTGRES_USER=hololive_collector_a\n'
    printf 'POSTGRES_HOST=%s\n' "$AP_POSTGRES_HOST"
    printf 'POSTGRES_PORT=%s\n' "$AP_POSTGRES_PORT"
    printf 'POSTGRES_DB=hololive\n'
    printf 'POSTGRES_SSLMODE=verify-full\n'
    printf 'POSTGRES_SSLROOTCERT=/etc/stack-secrets/hololive-bot/certs/postgres-ca.pem\n'
    printf 'POSTGRES_QUERY_EXEC_MODE=cache_statement\n'
    printf 'POSTGRES_POOL_MIN_CONNS=2\n'
    printf 'POSTGRES_POOL_MAX_CONNS=%s\n' "$AP_POSTGRES_POOL_MAX_CONNS"
    printf 'POSTGRES_SOCKET_PATH=\n'
    printf 'GOMEMLIMIT=384MiB\n'
    printf 'GOGC=100\n'
    printf 'GIN_MODE=release\n'
    printf 'LOG_DIR=/var/log/hololive-bot\n'
    printf 'LOG_LEVEL=info\n'
  } > "$dest"
}

write_wrapper() {
  local dest="$1"
  cp "$COLLECTOR_WRAPPER_LIB" "$dest"
  chmod 0755 "$dest"
}

render_po_unit() { sed "s/@RELEASE_ID@/$release_id/g" "$PO_UNIT_TEMPLATE"; }
stream_sha256() { sha256sum | cut -d' ' -f1; }

build_native_payload() {
  mkdir -p "$artifact_dir/bin"
  artifact_dir="$(cd "$artifact_dir" && pwd)"
  for helper in ap-host-native-{release-path,po,cutover,rollback-check,window}.sh \
    compose-env.sh postgres-endpoint.sh worker-profile-pair.py; do
    cp "$REPO_ROOT/scripts/deploy/lib/$helper" "$artifact_dir/bin/"
  done
  sh "$REPO_ROOT/scripts/build/build-hololive-collector-go.sh" \
    --output-dir "$artifact_dir" --version "$version" --revision "$native_revision" \
    --goos linux --goarch amd64 --goamd64 "$AP_GOAMD64"
  verify_native_collector
  local issuer_artifact="$artifact_dir/po-sandbox-build"
  "$REPO_ROOT/scripts/build/build-po-sandbox-artifact.sh" amd64 "$native_revision" "$version" "$issuer_artifact" rootfs
  mkdir -p "$artifact_dir/po-sandbox"
  cp "$issuer_artifact/rootfs.tar" "$issuer_artifact/rootfs.tar.sha256" \
    "$issuer_artifact/rootfs-manifest.json" "$issuer_artifact/revision" \
    "$issuer_artifact/architecture" "$issuer_artifact/version" "$artifact_dir/po-sandbox/"
  cp "$REPO_ROOT/scripts/build/po-sandbox-manifest.py" "$artifact_dir/bin/po-sandbox-manifest.py"
  rm -rf "$issuer_artifact"
  write_wrapper "$artifact_dir/bin/hololive-collector-wrapper"
  rm -rf "$artifact_dir/youtubejs"
  mkdir -p "$artifact_dir/youtubejs"
  cp "$REPO_ROOT/hololive/hololive-collector/youtubejs/package.json" \
    "$REPO_ROOT/hololive/hololive-collector/youtubejs/package-lock.json" "$artifact_dir/youtubejs/"
  cp -R "$REPO_ROOT/hololive/hololive-collector/youtubejs/src" "$artifact_dir/youtubejs/src"
  rm -f "$artifact_dir/youtubejs/src/"*.test.mjs
  (
    cd "$artifact_dir/youtubejs"
    npm_config_engine_strict=true npm exec --yes --package=corepack@0.36.0 -- corepack npm ci --omit=dev --no-audit --no-fund --ignore-scripts
  )
  write_host_env "$artifact_dir/${slot_host_env##*/}"
  cp "$UNIT_TEMPLATE" "$artifact_dir/${slot_collector_unit%%@*}@.service"
  render_po_unit > "$artifact_dir/$slot_issuer_unit"
  cp "$PO_SOCKET_TEMPLATE" "$artifact_dir/${slot_issuer_unit%.service}.socket"
}

verify_native_collector() {
  sh "$REPO_ROOT/scripts/build/check-hololive-collector-go-artifact.sh" "$artifact_dir" \
    --version "$version" --revision "$native_revision" --goos linux --goarch amd64 --goamd64 "$AP_GOAMD64"
}

apply_remote() {
  {
    printf '%s\n' "$AP_SLOT_PREAMBLE"
    printf 'set -euo pipefail\nsudo -n bash -s -- %q' "$slot_root"
    printf " <<'NATIVE_CUSTODY'\n"
    native_tool_blob scripts/deploy/verify-exec-tree-ownership.sh || return "$?"
    printf '\nNATIVE_CUSTODY\n'
    local fragment
    for fragment in "${CUTOVER_LIBS[@]}"; do native_tool_blob "scripts/deploy/lib/$fragment.sh" || return "$?"; done
  } |
    ap_remote_bash "$payload_name" "$release_id" "$service" "$port" "${change_started_at:-}" "$AP_REQUIRED_UDP_BUFFER_BYTES" "$AP_SWAPFILE_SIZE_MIB" "$native_revision" "$1" "${window_args[@]}"
}

window_args=()
if [[ "$MODE" == --apply ]]; then
  window_args+=("$(write_host_env /dev/stdout | stream_sha256)")
  window_args+=("$(stream_sha256 < "$UNIT_TEMPLATE")")
  window_args+=("$(render_po_unit | stream_sha256)")
  window_args+=("$(stream_sha256 < "$PO_SOCKET_TEMPLATE")")
  origin="$(apply_remote apply-origin)"
  native_release_id_validate "${origin%% *}"
  [[ "${origin#* }" =~ ^[0-9a-f]{40}\ [0-9]+:[0-9]+:[0-9]+$ ]]
  window_args=("$origin")
  mkdir -p -- "$(dirname -- "$artifact_dir")"
  mkdir -- "$artifact_dir"
fi
build_native_payload
if [[ "$MODE" == --apply ]]; then
  digest="$(native_payload_digest "$artifact_dir")"
  window_args+=("$digest")
  printf 'native attempt: release=%s payload_sha256=%s\n' "$release_id" "$digest" >&2
fi

RSYNC_RSH="$(ap_rsync_rsh)"

if [[ "$MODE" == "--dry-run" ]]; then
  rsync -ani --delete "$artifact_dir/" -e "$RSYNC_RSH" "$(ap_rsync_target "./$payload_name/")"
  digest="$(native_payload_digest "$artifact_dir")"
  printf 'HOLO_NATIVE_PAYLOAD_SHA256=%s\n' "$digest"
  echo "[DRY-RUN] Built $artifact_dir; no remote files or services changed."
  exit 0
fi

ap_remote_bash "$payload_name" <<'REMOTE'
set -eu
mkdir -m 0700 -- "$HOME/$1"
REMOTE

rsync -ai --delete "$artifact_dir/" -e "$RSYNC_RSH" "$(ap_rsync_target "./$payload_name/")"
change_started_at="$(ap_remote_bash <<'REMOTE'
date -u +%Y-%m-%dT%H:%M:%SZ
REMOTE
)"

apply_cutover() {
  local nonce
  nonce="$(apply_remote same-window)" || return "$?"
  [[ "$nonce" =~ ^[0-9a-f-]{36}$ ]] || return 1
  window_args=("$nonce")
}
apply_cutover || {
  status=$?
  if (( status == 255 )); then
    echo 'native cutover outcome unknown after transport loss; do not retry or release a guard' >&2
  fi
  exit "$status"
}

for observer in "$tool_root/scripts/logs/ap-host-native-status.sh" "$tool_root/scripts/deploy/ap-completion-check.sh"; do
  REPO_ROOT="$REPO_ROOT" CHANGE_STARTED_AT="$change_started_at" "$observer" "$AP_NAME" || {
    status=$?
    echo 'native post-cutover observation failed; current state unconfirmed; no automatic change' >&2
    exit "$status"
  }
done
apply_remote apply-accepted
