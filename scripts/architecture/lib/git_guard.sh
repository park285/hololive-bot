#!/usr/bin/env bash
set -euo pipefail

require_git_checkout() {
  local root_dir="$1"
  if ! git -C "${root_dir}" rev-parse --is-inside-work-tree >/dev/null 2>&1; then
    echo "FAIL: git checkout required for this script: ${root_dir}" >&2
    exit 1
  fi
}
