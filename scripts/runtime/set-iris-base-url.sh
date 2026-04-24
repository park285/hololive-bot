#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 1 ]]; then
  echo "usage: $0 <iris-base-url>" >&2
  exit 1
fi

base_url="${1%/}"

if [[ ! "$base_url" =~ ^https?://[^/[:space:]]+(:[0-9]+)?$ ]]; then
  echo "error: iris base url must match http[s]://host[:port]" >&2
  exit 1
fi

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
runtime_dir="$repo_root/runtime-config"
target_file="$runtime_dir/iris_base_url"
tmp_file="$target_file.tmp"

mkdir -p "$runtime_dir"
printf '%s\n' "$base_url" > "$tmp_file"
mv "$tmp_file" "$target_file"

echo "updated $target_file"
echo "current iris base url: $base_url"
