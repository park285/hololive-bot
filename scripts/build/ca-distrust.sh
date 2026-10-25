#!/bin/sh
# CA bundle에서 ca-distrust.sha256의 루트를 빼고 나머지 블록을 원래 순서대로 씁니다.
# 이미지 빌드 단계에서 실행해, 배포 진입점이 이미지를 다시 빌드해도 신뢰 차단이 빠지지 않게 합니다.
set -eu

[ $# -eq 2 ] || { echo "usage: $0 <input-bundle> <output-bundle>" >&2; exit 2; }
input=$1
output=$2
list="$(dirname "$0")/ca-distrust.sha256"
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

# 블록 사이의 빈 줄만 버리고, 그 밖의 PEM 아닌 내용은 조용히 버리지 않고 실패합니다.
awk -v dir="$work" '
  /^-----BEGIN CERTIFICATE-----$/ { n++; file = sprintf("%s/%06d.pem", dir, n) }
  file == "" && NF == 0 { next }
  file == "" { bad = 1; exit }
  { print > file }
  /^-----END CERTIFICATE-----$/ { close(file); file = "" }
  END { if (bad || file != "" || n == 0) exit 3 }
' "$input" || { echo "ca-distrust: $input is not a plain PEM certificate bundle" >&2; exit 1; }

distrusted=$(sed -e 's/#.*//' "$list" | awk 'NF { print $1 }')
umask 022
: > "$output"
removed=0
for block in "$work"/*.pem; do
  fingerprint=$(sed '1d;$d' "$block" | base64 -d | sha256sum | cut -d' ' -f1)
  if printf '%s\n' "$distrusted" | grep -qx "$fingerprint"; then
    removed=$((removed + 1))
  else
    cat "$block" >> "$output"
  fi
done

[ "$removed" -gt 0 ] || { echo "ca-distrust: no listed root found in $input; review $list" >&2; exit 1; }
echo "ca-distrust: removed $removed root(s) from $input"
