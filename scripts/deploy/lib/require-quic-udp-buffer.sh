#!/usr/bin/env bash
set -euo pipefail
required="${1:?required udp buffer bytes}"
host_name="${2:-AP host}"
sysctl_root="${AP_SYSCTL_ROOT:-}"
[[ "$required" =~ ^[0-9]+$ ]] || { echo "required udp buffer bytes must be an integer" >&2; exit 2; }
current_value() { sysctl -n "$1" 2>/dev/null || echo 0; }
# sysctl --system 적용 의미론을 따른다: /etc/sysctl.d/*.conf를 lexical 순서로 적용한 뒤
# /etc/sysctl.conf가 마지막에 적용되므로 같은 키는 마지막 대입(last-wins)이 실효값이다.
effective_persisted_value() {
  local key="$1" value="" file line candidate regex
  regex="^[[:space:]]*${key//./\\.}[[:space:]]*="
  shopt -s nullglob
  local files=("${sysctl_root}/etc/sysctl.d/"*.conf "${sysctl_root}/etc/sysctl.conf")
  for file in "${files[@]}"; do
    [[ -r "$file" ]] || continue
    while IFS= read -r line || [[ -n "$line" ]]; do
      line="${line%%#*}"
      [[ "$line" =~ $regex ]] || continue
      candidate="${line#*=}"; candidate="${candidate//[[:space:]]/}"
      if [[ "$candidate" =~ ^[0-9]+$ ]]; then value="$candidate"; fi
    done < "$file"
  done
  printf '%s\n' "${value:-0}"
}
rmem_max="$(current_value net.core.rmem_max)"
wmem_max="$(current_value net.core.wmem_max)"
if (( rmem_max < required || wmem_max < required )); then
  echo "AP QUIC UDP buffers too small on ${host_name}: net.core.rmem_max=${rmem_max} net.core.wmem_max=${wmem_max} required>=${required}" >&2
  exit 1
fi
persisted_rmem_max="$(effective_persisted_value net.core.rmem_max)"
persisted_wmem_max="$(effective_persisted_value net.core.wmem_max)"
if (( persisted_rmem_max < required || persisted_wmem_max < required )); then
  echo "AP QUIC UDP buffers are not persisted on ${host_name}: persisted net.core.rmem_max=${persisted_rmem_max} net.core.wmem_max=${persisted_wmem_max} required>=${required}" >&2
  exit 1
fi
echo "AP QUIC UDP buffers ok on ${host_name}: runtime rmem=${rmem_max} wmem=${wmem_max}; persisted rmem=${persisted_rmem_max} wmem=${persisted_wmem_max}"
