#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
root="$repo_root"
args=()
while (($#)); do
  case "$1" in
    --root) root="$2"; args+=("$1" "$2"); shift 2 ;;
    *) args+=("$1"); shift ;;
  esac
done

exec python3 "$repo_root/scripts/architecture/check-structure-budget.py" \
  --root "$root" \
  --policy "$root/scripts/architecture/structure-budget-policy.json" \
  "${args[@]}"
