#!/usr/bin/env bash
set -euo pipefail

root_dir="$(git rev-parse --show-toplevel)"
manifest="$root_dir/scripts/ci/npm-audit-manifest.txt"

while IFS= read -r lockfile; do
  [[ -n "$lockfile" ]] || continue
  [[ "$lockfile" == */package-lock.json && -f "$root_dir/$lockfile" ]] || {
    echo "invalid npm audit manifest entry: $lockfile" >&2
    exit 1
  }
  echo "npm audit: $lockfile"
  (
    cd "$root_dir/${lockfile%/package-lock.json}"
    npm audit --package-lock-only --audit-level=high
  )
done <"$manifest"
