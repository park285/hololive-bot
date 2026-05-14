#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="${REPO_ROOT:-$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)}"
SSH_KEY="${SSH_KEY:-$REPO_ROOT/KR.key}"
OSAKA_HOST="${OSAKA_HOST:-kapu-iris-osaka-1}"
SSH_OSAKA=(ssh -F /dev/null -i "$SSH_KEY" -o IdentitiesOnly=yes -o SetEnv=LC_ALL=C -o SetEnv=LANG=C "ubuntu@$OSAKA_HOST")

"${SSH_OSAKA[@]}" 'sudo tee /usr/local/sbin/hololive-osaka-log-rollup.sh >/dev/null' <<'REMOTE_SCRIPT'
#!/usr/bin/env bash
set -euo pipefail

log_dir="${LOG_DIR:-/home/ubuntu/hololive-bot/logs}"

install -d -o opc -g docker -m 2770 "$log_dir" "$log_dir/archive"
chown opc:docker "$log_dir" "$log_dir/archive"
chmod 2770 "$log_dir" "$log_dir/archive"

printf 'log_rollup_disabled at=%s action=%s note=%s\n' \
  "$(date -Is)" "no_truncate_no_archive_no_delete" "app_log_files_are_left_in_place"
REMOTE_SCRIPT

"${SSH_OSAKA[@]}" 'sudo chmod 0755 /usr/local/sbin/hololive-osaka-log-rollup.sh'

"${SSH_OSAKA[@]}" 'sudo tee /etc/systemd/system/hololive-osaka-log-rollup.service >/dev/null' <<'REMOTE_SERVICE'
[Unit]
Description=Disabled hololive Osaka AP file log rollup

[Service]
Type=oneshot
ExecStart=/usr/local/sbin/hololive-osaka-log-rollup.sh
REMOTE_SERVICE

"${SSH_OSAKA[@]}" 'sudo systemctl daemon-reload &&
  sudo systemctl disable --now hololive-osaka-log-rollup.timer >/dev/null 2>&1 || true &&
  sudo rm -f /etc/systemd/system/hololive-osaka-log-rollup.timer &&
  sudo ln -s /dev/null /etc/systemd/system/hololive-osaka-log-rollup.timer &&
  sudo systemctl daemon-reload &&
  systemctl is-enabled hololive-osaka-log-rollup.timer || true &&
  systemctl is-active hololive-osaka-log-rollup.timer || true'
