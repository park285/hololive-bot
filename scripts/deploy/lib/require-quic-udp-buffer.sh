#!/usr/bin/env bash
set -euo pipefail
required="${1:?required udp buffer bytes}"
host_name="${2:-AP host}"
[[ "$required" =~ ^[0-9]+$ ]] || { echo "required udp buffer bytes must be an integer" >&2; exit 2; }
current_value() { sysctl -n "$1" 2>/dev/null || echo 0; }
max_persisted_value() {
  local key="$1" max=0 file line value regex
  regex="^[[:space:]]*${key//./\\.}[[:space:]]*="
  shopt -s nullglob
  for file in /etc/sysctl.conf /etc/sysctl.d/*.conf; do
    [[ -r "$file" ]] || continue
    while IFS= read -r line || [[ -n "$line" ]]; do
      line="${line%%#*}"
      [[ "$line" =~ $regex ]] || continue
      value="${line#*=}"; value="${value//[[:space:]]/}"
      if [[ "$value" =~ ^[0-9]+$ && "$value" -gt "$max" ]]; then max="$value"; fi
    done < "$file"
  done
  printf '%s\n' "$max"
}
rmem_max="$(current_value net.core.rmem_max)"
wmem_max="$(current_value net.core.wmem_max)"
if (( rmem_max < required || wmem_max < required )); then
  echo "AP QUIC UDP buffers too small on ${host_name}: net.core.rmem_max=${rmem_max} net.core.wmem_max=${wmem_max} required>=${required}" >&2
  exit 1
fi
persisted_rmem_max="$(max_persisted_value net.core.rmem_max)"
persisted_wmem_max="$(max_persisted_value net.core.wmem_max)"
if (( persisted_rmem_max < required || persisted_wmem_max < required )); then
  echo "AP QUIC UDP buffers are not persisted on ${host_name}: persisted net.core.rmem_max=${persisted_rmem_max} net.core.wmem_max=${persisted_wmem_max} required>=${required}" >&2
  exit 1
fi
echo "AP QUIC UDP buffers ok on ${host_name}: runtime rmem=${rmem_max} wmem=${wmem_max}; persisted rmem=${persisted_rmem_max} wmem=${persisted_wmem_max}"
