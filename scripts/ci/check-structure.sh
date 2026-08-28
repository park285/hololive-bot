#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "${script_dir}/../.." && pwd)"
. "${script_dir}/python-runtime.sh"
repo_python_init
root="$repo_root"
args=()
while (($#)); do
  case "$1" in
    --root) root="$2"; args+=("$1" "$2"); shift 2 ;;
    *) args+=("$1"); shift ;;
  esac
done

exec "${CI_PYTHON_BIN}" "$repo_root/scripts/architecture/check-structure-budget.py" \
  --root "$root" \
  --policy "$root/scripts/architecture/structure-budget-policy.json" \
  "${args[@]}"
