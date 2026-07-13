#!/usr/bin/env bash

validate_nilaway_parallel() {
  local value="${1-}"
  if [[ "${value}" =~ ^[1-2]$ ]]; then
    return 0
  fi
  printf 'invalid NILAWAY_PARALLEL=%q; expected 1 or 2\n' "${value}" >&2
  return 1
}

validate_nilaway_gomemlimit() {
  local value="${1-}"
  if [[ "${value}" =~ ^(off|[0-9]+(([KMGT]i)?B)?)$ ]]; then
    return 0
  fi
  printf 'invalid NILAWAY_GOMEMLIMIT=%q; expected off or ^[0-9]+(([KMGT]i)?B)?$\n' "${value}" >&2
  return 1
}
