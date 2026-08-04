#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="${REPO_ROOT:-$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)}"
OPENBAO_STACK_ROOT="${OPENBAO_STACK_ROOT:-/home/kapu/work/openbao-secrets-stack}"
MODE="${2:---dry-run}"
REUSE_REMOTE_OPENBAO_CREDENTIALS="${REUSE_REMOTE_OPENBAO_CREDENTIALS:-false}"

case "$MODE" in
  --dry-run|--apply) ;;
  *)
    echo "Usage: $0 <ap-host> [--dry-run|--apply]" >&2
    exit 2
    ;;
esac
case "$REUSE_REMOTE_OPENBAO_CREDENTIALS" in
  true|false) ;;
  *)
    echo "REUSE_REMOTE_OPENBAO_CREDENTIALS must be true or false" >&2
    exit 2
    ;;
esac

. "$REPO_ROOT/scripts/deploy/lib/ap-host.sh"
ap_host_load "$REPO_ROOT" "${1:-}"

if [[ "$MODE" == "--apply" && "${!AP_APPROVE_DEPLOY_VAR:-}" != "true" ]]; then
  echo "Refusing apply without $AP_APPROVE_DEPLOY_VAR=true" >&2
  exit 2
fi

ROLE_ID_FILE="${ROLE_ID_FILE:-$OPENBAO_STACK_ROOT/out/hololive-bot-ap-prod.role_id}"
SECRET_ID_FILE="${SECRET_ID_FILE:-$OPENBAO_STACK_ROOT/out/hololive-bot-ap-prod.secret_id}"
OPENBAO_CA_FILE="${OPENBAO_CA_FILE:-/opt/secrets-stack/openbao/tls/server.crt}"
readonly BAO_AMD64_SHA256=736b8ecf354fda6b2af62e4ae064f12fe6c52d7db8425b9c6de22f286a5485ec
readonly BAO_ARM64_SHA256=994a10a7d42f750345ec815540747022cfc6b5a2dd707f535a6ea45d6eac2bfd

remote_arch="$("${AP_SSH[@]}" uname -m)"
case "$remote_arch" in
  x86_64)
    default_bao_bin="$(command -v bao || true)"
    expected_bao_sha256="$BAO_AMD64_SHA256"
    ;;
  aarch64)
    default_bao_bin="$OPENBAO_STACK_ROOT/artifacts/openbao/2.6.1/bao-linux-arm64"
    expected_bao_sha256="$BAO_ARM64_SHA256"
    ;;
  *)
    echo "unsupported AP architecture: $remote_arch" >&2
    exit 1
    ;;
esac
BAO_BIN="${BAO_BIN:-$default_bao_bin}"

required_files=(
  "$OPENBAO_STACK_ROOT/config/agent-hololive-bot-ap.hcl"
  "$OPENBAO_STACK_ROOT/deploy/systemd/openbao-agent-hololive-bot-ap.service"
  "$OPENBAO_STACK_ROOT/scripts/verify-hololive-h3-contract.sh"
)
for file in "${required_files[@]}"; do
  [[ -r "$file" ]] || {
    echo "required file not readable: $file" >&2
    exit 1
  }
done
[[ -n "$BAO_BIN" && -x "$BAO_BIN" ]] || {
  echo "bao binary not found; set BAO_BIN" >&2
  exit 1
}
actual_bao_sha256="$(sha256sum "$BAO_BIN" | cut -d' ' -f1)"
[[ "$actual_bao_sha256" == "$expected_bao_sha256" ]] || {
  echo "bao binary does not match the approved OpenBao 2.6.1 hash for $remote_arch" >&2
  exit 1
}
if [[ "$MODE" == "--apply" && "$REUSE_REMOTE_OPENBAO_CREDENTIALS" != true ]]; then
  [[ -r "$ROLE_ID_FILE" ]] || {
    echo "role_id file not readable: $ROLE_ID_FILE" >&2
    exit 1
  }
  [[ -r "$SECRET_ID_FILE" ]] || {
    echo "secret_id file not readable: $SECRET_ID_FILE" >&2
    exit 1
  }
fi

payload_dir="$(mktemp -d)"
payload_name=".hololive-openbao-agent-${AP_NAME}-$(date -u +%Y%m%dT%H%M%SZ)"
cleanup() {
  rm -rf "$payload_dir"
}
trap cleanup EXIT

install -m 0755 "$BAO_BIN" "$payload_dir/bao"
sudo -n install -m 0644 "$OPENBAO_CA_FILE" "$payload_dir/ca.crt"
install -m 0640 "$OPENBAO_STACK_ROOT/config/agent-hololive-bot-ap.hcl" "$payload_dir/hololive-bot.hcl"
install -m 0644 "$OPENBAO_STACK_ROOT/deploy/systemd/openbao-agent-hololive-bot-ap.service" "$payload_dir/openbao-agent-hololive-bot.service"
install -m 0755 "$OPENBAO_STACK_ROOT/scripts/verify-hololive-h3-contract.sh" "$payload_dir/verify-hololive-h3-contract"
if [[ "$MODE" == "--apply" && "$REUSE_REMOTE_OPENBAO_CREDENTIALS" != true ]]; then
  install -m 0640 "$ROLE_ID_FILE" "$payload_dir/hololive-bot-ap.role_id"
  install -m 0600 "$SECRET_ID_FILE" "$payload_dir/hololive-bot-ap.secret_id"
fi

RSYNC_RSH="ssh -F /dev/null -i $SSH_KEY -o IdentitiesOnly=yes"
if [[ -n "$AP_SSH_HOST_KEY_ALIAS" ]]; then
  RSYNC_RSH+=" -o HostKeyAlias=$AP_SSH_HOST_KEY_ALIAS"
fi

if [[ "$MODE" == "--dry-run" ]]; then
  rsync -ani --delete "$payload_dir/" -e "$RSYNC_RSH" "ubuntu@$AP_SSH_HOST:~/$payload_name/"
  echo "[DRY-RUN] No remote files, credentials, or services changed."
  exit 0
fi

rsync -ai --delete "$payload_dir/" -e "$RSYNC_RSH" "ubuntu@$AP_SSH_HOST:~/$payload_name/"

change_started_at="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
ap_remote_bash \
  "$payload_name" "$REUSE_REMOTE_OPENBAO_CREDENTIALS" "$AP_RUNTIME_MODE" \
  "${AP_SERVICES[0]}" "${AP_CONTAINERS[0]}" "${AP_PORTS[0]}" \
  "$AP_COMPOSE_FILE" <<'REMOTE'
set -euo pipefail
payload_name="$1"
reuse_remote_credentials="$2"
runtime_mode="$3"
consumer_service="$4"
consumer_container="$5"
consumer_port="$6"
ap_compose_file="$7"
payload="$HOME/$payload_name"
cleanup_remote() {
  rm -rf -- "$payload"
}
trap cleanup_remote EXIT
unit=openbao-agent-hololive-bot.service
previous_start_monotonic="$(sudo -n systemctl show "$unit" -p ExecMainStartTimestampMonotonic --value 2>/dev/null || true)"
previous_cert_generation="$(sudo -n stat -c '%Y:%i' /run/hololive-bot/certs/hololive-h3.crt 2>/dev/null || true)"
apply_started_epoch="$(date +%s)"

if ! getent group opc >/dev/null; then
  sudo -n groupadd --system opc
fi
if ! getent passwd openbao-agent-hololive-bot-ap >/dev/null; then
  sudo -n useradd --system --user-group --no-create-home --home-dir /nonexistent \
    --shell /usr/sbin/nologin openbao-agent-hololive-bot-ap
fi

sudo -n install -d -m 0711 -o root -g root /etc/openbao-agent
sudo -n install -m 0755 "$payload/bao" /usr/bin/bao
sudo -n install -m 0444 -o root -g root "$payload/ca.crt" /etc/openbao-agent/ca.crt
sudo -n install -m 0400 -o openbao-agent-hololive-bot-ap -g openbao-agent-hololive-bot-ap "$payload/hololive-bot.hcl" /etc/openbao-agent/hololive-bot.hcl
if [[ "$reuse_remote_credentials" == true ]]; then
  for credential in hololive-bot-ap.role_id hololive-bot-ap.secret_id; do
    sudo -n test -s "/etc/openbao-agent/$credential"
    [[ "$(sudo -n stat -c '%a %U %G' "/etc/openbao-agent/$credential")" == \
      "400 openbao-agent-hololive-bot-ap openbao-agent-hololive-bot-ap" ]]
  done
else
  sudo -n install -m 0400 -o openbao-agent-hololive-bot-ap -g openbao-agent-hololive-bot-ap "$payload/hololive-bot-ap.role_id" /etc/openbao-agent/hololive-bot-ap.role_id
  sudo -n install -m 0400 -o openbao-agent-hololive-bot-ap -g openbao-agent-hololive-bot-ap "$payload/hololive-bot-ap.secret_id" /etc/openbao-agent/hololive-bot-ap.secret_id
fi
sudo -n install -m 0755 "$payload/verify-hololive-h3-contract" /usr/local/sbin/verify-hololive-h3-contract
sudo -n install -m 0644 "$payload/openbao-agent-hololive-bot.service" /etc/systemd/system/openbao-agent-hololive-bot.service
sudo -n install -d -m 0750 -o openbao-agent-hololive-bot-ap -g opc /run/hololive-bot /run/hololive-bot/certs

sudo -n systemd-analyze verify /etc/systemd/system/openbao-agent-hololive-bot.service
sudo -n systemctl daemon-reload
sudo -n systemctl enable openbao-agent-hololive-bot.service
sudo -n systemctl restart openbao-agent-hololive-bot.service

for _ in $(seq 1 30); do
  if sudo -n test -r /run/hololive-bot/ap-compose.env &&
     sudo -n test -r /run/hololive-bot/youtube-producer.env &&
     sudo -n test -r /run/hololive-bot/certs/postgres-ca.pem &&
     sudo -n test -r /run/hololive-bot/certs/hololive-h3.crt &&
     sudo -n test -r /run/hololive-bot/certs/hololive-h3.key; then
    candidate_start_monotonic="$(sudo -n systemctl show "$unit" -p ExecMainStartTimestampMonotonic --value)"
    candidate_cert_generation="$(sudo -n stat -c '%Y:%i' /run/hololive-bot/certs/hololive-h3.crt)"
    candidate_cert_mtime="$(sudo -n stat -c '%Y' /run/hololive-bot/certs/hololive-h3.crt)"
    process_is_fresh=true
    cert_is_fresh=true
    if [[ -n "$previous_start_monotonic" && "$previous_start_monotonic" != 0 &&
          "$candidate_start_monotonic" == "$previous_start_monotonic" ]]; then
      process_is_fresh=false
    fi
    if [[ -n "$previous_cert_generation" &&
          "$candidate_cert_generation" == "$previous_cert_generation" ]]; then
      cert_is_fresh=false
    fi
    if [[ "$process_is_fresh" == true && "$cert_is_fresh" == true &&
          "$candidate_cert_mtime" -ge "$apply_started_epoch" ]]; then
      break
    fi
  fi
  sleep 2
done

current_start_monotonic="$(sudo -n systemctl show "$unit" -p ExecMainStartTimestampMonotonic --value)"
current_cert_generation="$(sudo -n stat -c '%Y:%i' /run/hololive-bot/certs/hololive-h3.crt)"
current_cert_mtime="$(sudo -n stat -c '%Y' /run/hololive-bot/certs/hololive-h3.crt)"
[[ "$(sudo -n systemctl is-active "$unit")" == active ]]
[[ -n "$current_start_monotonic" && "$current_start_monotonic" != 0 ]]
if [[ -n "$previous_start_monotonic" && "$previous_start_monotonic" != 0 ]]; then
  [[ "$current_start_monotonic" != "$previous_start_monotonic" ]] || {
    echo "OpenBao Agent process generation did not change" >&2
    exit 1
  }
fi
if [[ -n "$previous_cert_generation" ]]; then
  [[ "$current_cert_generation" != "$previous_cert_generation" ]] || {
    echo "OpenBao Agent certificate render generation did not change" >&2
    exit 1
  }
fi
(( current_cert_mtime >= apply_started_epoch )) || {
  echo "OpenBao Agent certificate predates this apply" >&2
  exit 1
}

sudo -n /usr/local/sbin/verify-hololive-h3-contract --runtime-ap
if [[ "$runtime_mode" == native ]]; then
  consumer_unit="hololive-youtube-producer@${consumer_service}.service"
  sudo -n systemctl restart "$consumer_unit"
  for _ in $(seq 1 30); do
    if systemctl is-active --quiet "$consumer_unit" &&
       ss -lun | grep -Fq "127.0.0.1:${consumer_port}"; then
      break
    fi
    sleep 2
  done
  systemctl is-active --quiet "$consumer_unit"
  ss -lun | grep -Fq "127.0.0.1:${consumer_port}"
else
  cd "$HOME/hololive-bot"
  sudo -n env COMPOSE_ENV_FILE=/run/hololive-bot/ap-compose.env COMPOSE_PROFILES=oracle \
    ./scripts/deploy/compose.sh \
    -f deploy/compose/docker-compose.prod.yml -f "$ap_compose_file" \
    up -d --no-deps --force-recreate "$consumer_service"
  for _ in $(seq 1 30); do
    if [[ "$(docker inspect -f '{{.State.Health.Status}}' "$consumer_container")" == healthy ]]; then
      break
    fi
    sleep 2
  done
  [[ "$(docker inspect -f '{{.State.Health.Status}}' "$consumer_container")" == healthy ]]
  python3 - "$consumer_service" "$consumer_container" "$ap_compose_file" <<'PY'
import json
import subprocess
import sys

service = sys.argv[1]
container = sys.argv[2]
ap_compose_file = sys.argv[3]
desired_document = json.loads(
    subprocess.run(
        [
            "sudo", "-n", "env",
            "COMPOSE_ENV_FILE=/run/hololive-bot/ap-compose.env",
            "COMPOSE_PROFILES=oracle",
            "./scripts/deploy/compose.sh",
            "-f", "deploy/compose/docker-compose.prod.yml",
            "-f", ap_compose_file,
            "config", "--format", "json",
        ],
        check=True,
        stdout=subprocess.PIPE,
    ).stdout
)
actual_document = json.loads(
    subprocess.run(
        ["docker", "inspect", container],
        check=True,
        stdout=subprocess.PIPE,
    ).stdout
)

desired = desired_document["services"][service].get("environment", {})
actual = dict(item.split("=", 1) for item in actual_document[0]["Config"]["Env"])
mismatched = sorted(
    key for key, value in desired.items() if actual.get(key) != ("" if value is None else str(value))
)
if mismatched:
    print("container environment mismatch keys: " + ", ".join(mismatched), file=sys.stderr)
    raise SystemExit(1)
print("container environment matches rendered Compose config")
PY
fi
REMOTE

CHANGE_STARTED_AT="$change_started_at" "$REPO_ROOT/scripts/deploy/ap-completion-check.sh" "$AP_NAME"
