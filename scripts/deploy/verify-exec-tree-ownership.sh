#!/usr/bin/env bash
set -euo pipefail

# 인자로 받은 각 경로가 root-owned 이고 비root 사용자에게 쓰기 불가인지 검증한다.
# root systemd 가 실행하는 entrypoint/sourced-lib/compose-YAML 이 kapu 등 비root 에게
# 쓰기 가능하면, 그 계정을 장악한 공격자가 다음 start/stop/boot 시 root 로 임의 코드를
# 실행할 수 있다(03e6dca8). non-root 소유는 소유자가 언제든 chmod 후 내용 교체가 가능하므로
# "쓰기 비트"가 아니라 "소유권"까지 root 여야 안전하다.

violations=0

report() {
  echo "[verify-exec-tree] $*" >&2
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

if [[ "$#" -eq 0 ]]; then
  report "usage: verify-exec-tree-ownership.sh <path> [path...]"
  exit 2
fi

for target in "$@"; do
  if [[ ! -e "${target}" ]]; then
    report "missing (skipped): ${target}"
    continue
  fi
  path_is_root_safe "${target}" || violations=$((violations + 1))
done

if (( violations > 0 )); then
  report "${violations} path(s) writable by a non-root user"
  exit 1
fi
exit 0
