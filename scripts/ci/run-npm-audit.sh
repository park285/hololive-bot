#!/usr/bin/env bash
set -euo pipefail

root_dir="$(git rev-parse --show-toplevel)"
manifest="$root_dir/scripts/ci/npm-audit-manifest.txt"
npm_package_manager="$(node -p 'require(process.argv[1]).packageManager' "$root_dir/admin-dashboard/frontend/package.json")"

if [[ ! "$npm_package_manager" =~ ^npm@[0-9]+\.[0-9]+\.[0-9]+\+sha512\.[0-9a-f]{128}$ ]]; then
  echo "frontend packageManager must be an exact npm version with sha512 integrity" >&2
  exit 1
fi

while IFS= read -r lockfile; do
  [[ -n "$lockfile" ]] || continue
  [[ "$lockfile" == */package-lock.json && -f "$root_dir/$lockfile" ]] || {
    echo "invalid npm audit manifest entry: $lockfile" >&2
    exit 1
  }
  echo "npm audit: $lockfile"
  (
    cd "$root_dir/${lockfile%/package-lock.json}"
    corepack "$npm_package_manager" audit --package-lock-only --audit-level=high
  )
done <"$manifest"
