#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="${REPO_ROOT:-$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)}"
OPENBAO_STACK_ROOT="${OPENBAO_STACK_ROOT:-/home/kapu/work/openbao-secrets-stack}"
MODE="${2:---dry-run}"

case "$MODE" in
  --dry-run|--apply) ;;
  *)
    echo "Usage: $0 <ap-host> [--dry-run|--apply]" >&2
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
BAO_BIN="${BAO_BIN:-$(command -v bao || true)}"
OPENBAO_CA_FILE="${OPENBAO_CA_FILE:-/opt/secrets-stack/openbao/tls/server.crt}"

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
if [[ "$MODE" == "--apply" ]]; then
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
if [[ "$MODE" == "--apply" ]]; then
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

ap_remote_bash "$payload_name" <<'REMOTE'
set -euo pipefail
payload_name="$1"
payload="$HOME/$payload_name"

if ! getent group opc >/dev/null; then
  sudo -n groupadd --system opc
fi

sudo -n install -d -m 0750 -o root -g root /etc/openbao-agent
sudo -n install -m 0755 "$payload/bao" /usr/bin/bao
sudo -n install -m 0644 "$payload/ca.crt" /etc/openbao-agent/ca.crt
sudo -n install -m 0640 -o root -g opc "$payload/hololive-bot.hcl" /etc/openbao-agent/hololive-bot.hcl
sudo -n install -m 0640 -o root -g opc "$payload/hololive-bot-ap.role_id" /etc/openbao-agent/hololive-bot-ap.role_id
sudo -n install -m 0600 -o root -g root "$payload/hololive-bot-ap.secret_id" /etc/openbao-agent/hololive-bot-ap.secret_id
sudo -n install -m 0755 "$payload/verify-hololive-h3-contract" /usr/local/sbin/verify-hololive-h3-contract
sudo -n install -m 0644 "$payload/openbao-agent-hololive-bot.service" /etc/systemd/system/openbao-agent-hololive-bot.service

sudo -n systemd-analyze verify /etc/systemd/system/openbao-agent-hololive-bot.service
sudo -n systemctl daemon-reload
sudo -n systemctl enable --now openbao-agent-hololive-bot.service

for _ in $(seq 1 30); do
  if sudo -n test -r /run/hololive-bot/ap-compose.env &&
     sudo -n test -r /run/hololive-bot/youtube-producer.env &&
     sudo -n test -r /run/hololive-bot/certs/postgres-ca.pem &&
     sudo -n test -r /run/hololive-bot/certs/hololive-h3.crt &&
     sudo -n test -r /run/hololive-bot/certs/hololive-h3.key; then
    break
  fi
  sleep 2
done

sudo -n /usr/local/sbin/verify-hololive-h3-contract --runtime-ap
sudo -n find /run/hololive-bot -maxdepth 3 -printf "%M %u %g %s %TY-%Tm-%TdT%TH:%TM:%TS %p\n" | sort
sudo -n sh -c 'for f in /run/hololive-bot/*.env; do test -r "$f" && printf "%s\n" "$f" && cut -d= -f1 "$f" | sort; done'
sudo -n openssl x509 -in /run/hololive-bot/certs/hololive-h3.crt -noout -subject -ext subjectAltName
sudo -n journalctl -u openbao-agent-hololive-bot.service -n 80 --no-pager |
  grep -E 'authentication successful|rendered|permission denied|x509|no such file|ERROR|WARN' || true
REMOTE
