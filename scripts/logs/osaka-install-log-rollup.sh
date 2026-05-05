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
archive_dir="${ARCHIVE_DIR:-$log_dir/archive}"
retention_days="${RETENTION_DAYS:-7}"
timestamp="$(date -u +%Y%m%dT%H%M%SZ)"
services=(youtube-scraper stream-ingester)

install -d -o opc -g docker -m 2770 "$log_dir" "$archive_dir"
chown opc:docker "$log_dir" "$archive_dir"
chmod 2770 "$log_dir" "$archive_dir"

for service in "${services[@]}"; do
  log_file="$log_dir/$service.log"
  [[ -s "$log_file" ]] || continue

  tmp_dir="$(mktemp -d)"
  trap 'rm -rf "$tmp_dir"' EXIT

  cp "$log_file" "$tmp_dir/$service.log"
  archive_tmp="$archive_dir/$service-$timestamp.log.tar.gz.tmp"
  archive_final="$archive_dir/$service-$timestamp.log.tar.gz"
  tar -C "$tmp_dir" -czf "$archive_tmp" "$service.log"
  chown opc:docker "$archive_tmp"
  chmod 0640 "$archive_tmp"
  mv "$archive_tmp" "$archive_final"

  : > "$log_file"
  chown opc:docker "$log_file"
  chmod 0660 "$log_file"
  rm -rf "$tmp_dir"
  trap - EXIT
done

find "$archive_dir" -type f \( -name '*.tar.gz' -o -name '*.gz' \) -mtime +"$retention_days" -delete
find "$archive_dir" -type d -empty -delete
install -d -o opc -g docker -m 2770 "$archive_dir"
chown opc:docker "$archive_dir"
chmod 2770 "$archive_dir"
REMOTE_SCRIPT

"${SSH_OSAKA[@]}" 'sudo chmod 0755 /usr/local/sbin/hololive-osaka-log-rollup.sh'

"${SSH_OSAKA[@]}" 'sudo tee /etc/systemd/system/hololive-osaka-log-rollup.service >/dev/null' <<'REMOTE_SERVICE'
[Unit]
Description=Roll up hololive Osaka AP file logs

[Service]
Type=oneshot
ExecStart=/usr/local/sbin/hololive-osaka-log-rollup.sh
REMOTE_SERVICE

"${SSH_OSAKA[@]}" 'sudo tee /etc/systemd/system/hololive-osaka-log-rollup.timer >/dev/null' <<'REMOTE_TIMER'
[Unit]
Description=Daily hololive Osaka AP log rollup

[Timer]
OnCalendar=*-*-* 03:15:00
Persistent=true
RandomizedDelaySec=10m

[Install]
WantedBy=timers.target
REMOTE_TIMER

"${SSH_OSAKA[@]}" 'sudo systemctl daemon-reload && sudo systemctl enable --now hololive-osaka-log-rollup.timer && sudo systemctl start hololive-osaka-log-rollup.service && systemctl is-enabled hololive-osaka-log-rollup.timer && systemctl is-active hololive-osaka-log-rollup.timer'
