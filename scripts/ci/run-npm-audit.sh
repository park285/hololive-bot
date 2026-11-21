#!/usr/bin/env bash
set -euo pipefail

root_dir="$(git rev-parse --show-toplevel)"
npm_package_manager="$(node -p 'require(process.argv[1]).packageManager' "$root_dir/hololive/hololive-youtube-collector/youtubejs/package.json")"

if [[ ! "$npm_package_manager" =~ ^npm@[0-9]+\.[0-9]+\.[0-9]+\+sha512\.[0-9a-f]{128}$ ]]; then
  echo "collector helper packageManager must be an exact npm version with sha512 integrity" >&2
  exit 1
fi

corepack_bin="$(cd "$root_dir" && npm exec --yes --package=corepack@0.36.0 -- sh -c 'command -v corepack')"

audit_lockfiles() {
  local file lockfile lock_path failed=0
  local -A tracked_locks=()
  local -a manifests=()
  while IFS= read -r -d '' file; do
    case "${file##*/}" in
      package-lock.json) tracked_locks["$file"]=1 ;;
      package.json) manifests+=("$file") ;;
    esac
  done
  for file in "${manifests[@]}"; do
    lockfile="${file%.json}-lock.json"
    if [[ ! -v 'tracked_locks[$lockfile]' ]]; then
      if ! jq -e '(.packageManager // "") | startswith("npm@") | not' "$root_dir/$file" >/dev/null; then
        printf 'npm project without tracked lockfile or unreadable manifest: %q\n' "$file" >&2
        failed=1
      fi
    fi
  done
  for lockfile in "${!tracked_locks[@]}"; do
    lock_path="$root_dir/$lockfile"
    if [[ ! -f "$lock_path" ]]; then
      printf 'missing tracked lockfile: %q\n' "$lockfile" >&2
      failed=1
      continue
    fi
    printf 'npm audit: %q\n' "$lockfile"
    (cd -- "${lock_path%/*}" && "$corepack_bin" "$npm_package_manager" audit --package-lock-only --audit-level=high) </dev/null || failed=1
  done
  return "$failed"
}

git -C "$root_dir" ls-files --error-unmatch -z -- ':(glob)**/package-lock.json' ':(glob)**/package.json' | audit_lockfiles
