#!/usr/bin/env bash
set -euo pipefail

# non-root 소유는 소유자가 chmod 후 내용을 언제든 교체할 수 있으므로, root systemd 실행
# 트리는 쓰기 비트가 아니라 소유권까지 root 여야 안전하다(03e6dca8).

violations=0

report() {
  echo "[verify-exec-tree] $*" >&2
}

lexical_abs() {
  python3 - "$1" <<'PY'
import os
import sys

print(os.path.abspath(os.path.normpath(sys.argv[1])))
PY
}

path_is_root_safe() {
  local f="$1" uid gid perms g o
  uid="$(stat -c '%u' "$f")"
  gid="$(stat -c '%g' "$f")"
  perms="$(printf '%04d' "$((10#$(stat -c '%a' "$f")))")"
  g="${perms:2:1}"
  o="${perms:3:1}"

  if [[ "${uid}" -ne 0 ]]; then
    report "NOT root-owned (uid=${uid}): ${f}"
    return 1
  fi
  if (( (o & 2) != 0 )); then
    report "other-writable (${perms}): ${f}"
    return 1
  fi
  if (( (g & 2) != 0 )) && [[ "${gid}" -ne 0 ]]; then
    report "group-writable by non-root group (gid=${gid}, ${perms}): ${f}"
    return 1
  fi
  return 0
}

path_chain() {
  local path="$1" dir
  path="$(lexical_abs "${path}")"
  dir="$(dirname "${path}")"
  while [[ "${dir}" != "/" ]]; do
    printf '%s\n' "${dir}"
    dir="$(dirname "${dir}")"
  done
  printf '/\n'
  printf '%s\n' "${path}"
}

path_chain_is_root_safe() {
  local target="$1" path violations=0
  while IFS= read -r path; do
    if [[ -L "${path}" ]]; then
      report "symlink in root exec path: ${path}"
      violations=$((violations + 1))
      continue
    fi
    path_is_root_safe "${path}" || violations=$((violations + 1))
  done < <(path_chain "${target}")

  (( violations == 0 ))
}

if [[ "$#" -eq 0 ]]; then
  report "usage: verify-exec-tree-ownership.sh <path> [path...]"
  exit 2
fi

for target in "$@"; do
  if [[ ! -e "${target}" ]]; then
    report "missing (skipped): ${target}"
    continue
  fi
  path_chain_is_root_safe "${target}" || violations=$((violations + 1))
done

if (( violations > 0 )); then
  report "${violations} path(s) writable by a non-root user"
  exit 1
fi
exit 0
