#!/usr/bin/env bash
set -euo pipefail

# 영구 계약(재도입 방지): 2026-08-25 퇴역한 youtube producer 이름이 저장소에 다시 들어오지 않게 막는 gate다.
# 퇴역 가드가 아니므로 gate 자체의 제거 조건은 없다(stack-audit 2026-09-26 T17 분류). allowlist 항목은 대상 경로가
# 사라지거나 더 이상 이름을 담지 않으면 함께 지우며, 아래 stale 검사가 이를 강제한다.

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
ALLOWLIST="${ROOT_DIR}/docs/current/architecture/youtube-producer-retirement.allowlist"
tmp_dir="$(mktemp -d)"
trap 'rm -rf "$tmp_dir"' EXIT

allowed_file="${tmp_dir}/allowed"
found_file="${tmp_dir}/found"
unexpected_file="${tmp_dir}/unexpected"
stale_file="${tmp_dir}/stale"

sed -e '/^[[:space:]]*#/d' -e '/^[[:space:]]*$/d' "$ALLOWLIST" | LC_ALL=C sort -u > "$allowed_file"

cd "$ROOT_DIR"
{
  git ls-files
  git ls-files --others --exclude-standard
} | LC_ALL=C sort -u | while IFS= read -r file_path; do
  case "$file_path" in
    docs/history/*|docs/current/architecture/youtube-producer-retirement.allowlist)
      continue
      ;;
  esac
  [[ -f "$file_path" ]] || continue
  if rg -I -q -i 'hololive-youtube-producer|youtube-producer|YOUTUBE_PRODUCER|YouTubeProducer' -- "$file_path"; then
    printf '%s\n' "$file_path"
  fi
done | LC_ALL=C sort -u > "$found_file"

LC_ALL=C comm -23 "$found_file" "$allowed_file" > "$unexpected_file"
LC_ALL=C comm -13 "$found_file" "$allowed_file" > "$stale_file"
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
