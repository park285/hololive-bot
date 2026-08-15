#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
ALLOWLIST="${ROOT_DIR}/docs/current/architecture/youtube-producer-retirement.allowlist"
tmp_dir="$(mktemp -d)"
trap 'rm -rf "$tmp_dir"' EXIT

allowed_file="${tmp_dir}/allowed"
found_file="${tmp_dir}/found"
unexpected_file="${tmp_dir}/unexpected"
stale_file="${tmp_dir}/stale"

sed -e '/^[[:space:]]*#/d' -e '/^[[:space:]]*$/d' "$ALLOWLIST" | sort -u > "$allowed_file"

cd "$ROOT_DIR"
{
  git ls-files
  git ls-files --others --exclude-standard
} | sort -u | while IFS= read -r file_path; do
  case "$file_path" in
    docs/history/*|docs/current/architecture/youtube-producer-retirement.allowlist)
      continue
      ;;
  esac
  [[ -f "$file_path" ]] || continue
  if rg -I -q -i 'hololive-youtube-producer|youtube-producer|YOUTUBE_PRODUCER|YouTubeProducer' -- "$file_path"; then
    printf '%s\n' "$file_path"
  fi
done | sort -u > "$found_file"

comm -23 "$found_file" "$allowed_file" > "$unexpected_file"
comm -13 "$found_file" "$allowed_file" > "$stale_file"
if [[ -s "$unexpected_file" ]]; then
  echo "unexpected retired youtube-producer references:" >&2
  sed 's/^/  /' "$unexpected_file" >&2
  exit 1
fi
if [[ -s "$stale_file" ]]; then
  echo "stale youtube-producer retirement allowlist entries:" >&2
  sed 's/^/  /' "$stale_file" >&2
  exit 1
fi

echo "youtube-producer retirement references match the exact allowlist"
