#!/usr/bin/env bash
set -euo pipefail

violations=0

report() {
  echo "[verify-exec-tree] $*" >&2
}

lexical_abs() {
  realpath -m -s -- "$1"
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
  if (( (g & 2) != 0 )); then
    report "group-writable (gid=${gid}, ${perms}): ${f}"
    return 1
  fi
  return 0
}

paths_with_acl() {
  python3 -I -S -c 'import os, sys
for path in sys.argv[1:]:
    if any(name.startswith("system.posix_acl_") for name in os.listxattr(path, follow_symlinks=False)):
        print(path)' "$@"
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
  local target="$1" path acl violations=0
  local -a checked=()
  while IFS= read -r path; do
    if [[ -L "${path}" ]]; then
      report "symlink in root exec path: ${path}"
      violations=$((violations + 1))
      continue
    fi
    checked+=("${path}")
    path_is_root_safe "${path}" || violations=$((violations + 1))
  done < <(path_chain "${target}")
  if ! acl="$(paths_with_acl "${checked[@]}")"; then
    report "cannot read POSIX ACLs: ${target}"
    violations=$((violations + 1))
  fi
  while IFS= read -r path; do
    [[ -n "${path}" ]] || continue
    report "POSIX ACL in root exec path: ${path}"
    violations=$((violations + 1))
  done <<<"${acl}"

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
