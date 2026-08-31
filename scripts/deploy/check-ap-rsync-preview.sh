#!/usr/bin/env bash
set -euo pipefail

preview_file="${1:?Usage: $0 <rsync-preview> [remote-repo-dir]}"
remote_repo_dir="${2:-hololive-bot}"

if [[ ! -r "$preview_file" ]]; then
  echo "rsync preview is not readable: $preview_file" >&2
  exit 1
fi

alarm_worker_dir="$remote_repo_dir/hololive/hololive-alarm-worker/"
alarm_worker_version="${alarm_worker_dir}VERSION"
forbidden=false

while IFS= read -r line; do
  item="${line%% *}"
  path="${line#"$item "}"

  if [[ "$path" == "$alarm_worker_dir" && "${item:1:1}" == "d" ]]; then
    continue
  fi
  if [[ "$path" == "$alarm_worker_version" && "${item:1:1}" == "f" ]]; then
    continue
  fi

  printf '%s\n' "$line" >&2
  forbidden=true
done < <(rg '(\.env|\.key|\.pem|hololive-alarm-worker|_test\.go|docs/|/logs/|/runtime-config/|/backups/|artifacts/)' "$preview_file" || true)

if [[ "$forbidden" == true ]]; then
  echo "rsync preview contains forbidden deployment scope" >&2
  exit 1
fi
