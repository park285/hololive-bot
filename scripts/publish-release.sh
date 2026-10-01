#!/usr/bin/env bash
set -euo pipefail

# 공개 본문은 GitHub 생성 결과만 사용하고 운영 요약은 별도 기록에 둡니다.
fail() { echo "[release] $*" >&2; exit 1; }
[[ $# -ge 2 && $# -le 3 ]] || fail 'usage: bash scripts/publish-release.sh <tag> <previous-tag> [preview|create|edit]'
tag=$1
previous=$2
mode=${3:-preview}
repo=park285/hololive-bot
for value in "$tag" "$previous"; do
  [[ "$value" =~ ^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$ ]] || fail "안정 SemVer tag가 아닙니다: $value"
done
[[ "$tag" != "$previous" ]] || fail '비교 기준과 대상 tag가 같습니다'
case "$mode" in preview|create|edit) ;; *) fail "알 수 없는 mode: $mode" ;; esac
for command in gh jq; do command -v "$command" >/dev/null || fail "$command 필요"; done
umask 077
evidence=$(mktemp -d "${TMPDIR:-/tmp}/hololive-release.XXXXXXXX")
echo "[release] 기록: $evidence" >&2
gh api "repos/$repo/git/ref/tags/$tag" > "$evidence/tag.json"
gh api "repos/$repo/git/ref/tags/$previous" > "$evidence/previous-tag.json"
gh api --paginate --slurp "repos/$repo/releases?per_page=100" > "$evidence/releases.json"
jq --arg tag "$tag" '[.[][] | select(.tag_name == $tag)] | if length > 1 then error("duplicate release") else .[0] end' \
  "$evidence/releases.json" > "$evidence/before.json"
exists=$(jq '. != null' "$evidence/before.json")
[[ "$mode" != create || "$exists" == false ]] || fail '이미 존재하는 Release입니다; edit을 사용하십시오'
[[ "$mode" != edit || "$exists" == true ]] || fail '수정 대상 Release가 없습니다'
# 기존 릴리즈 수정은 해당 게시 시각 이전의 직전 게시 릴리즈를 사용합니다.
expected=$(jq -r --arg tag "$tag" --slurpfile before "$evidence/before.json" '
  [.[][] | select(.draft == false and .tag_name != $tag)
    | select(($before[0].published_at == null) or (.published_at < $before[0].published_at))]
  | sort_by(.published_at) | last | .tag_name // empty' "$evidence/releases.json")
[[ -z "$expected" || "$previous" == "$expected" ]] || fail "직전 게시 릴리즈는 $expected 입니다"
gh api "repos/$repo/compare/$previous...$tag" > "$evidence/compare.json"
jq -e '.status == "ahead"' "$evidence/compare.json" >/dev/null || fail '대상 tag가 비교 기준보다 앞선 동일 계보여야 합니다'
gh api "repos/$repo/releases/generate-notes" -f tag_name="$tag" -f previous_tag_name="$previous" > "$evidence/generated.json"
jq -e '.body | type == "string" and length > 0' "$evidence/generated.json" >/dev/null || fail '자동 생성 본문이 비어 있습니다'
jq -j '.body' "$evidence/generated.json" > "$evidence/notes.md"
echo "[release] $repo: $previous -> $tag ($mode)" >&2
cat "$evidence/notes.md"
printf '\n'
[[ "$mode" != preview ]] || exit 0
if [[ "$mode" == create ]]; then
  [[ $(jq -r '.object.type' "$evidence/tag.json") == tag ]] || fail '신규 게시에는 annotated tag가 필요합니다'
  gh release create "$tag" --repo "$repo" --verify-tag --title "$tag" --notes-file "$evidence/notes.md"
else
  id=$(jq -r '.id' "$evidence/before.json")
  # 수정은 제목과 본문만 PATCH하여 tag·첨부물·공개 상태를 보존합니다.
  jq --arg name "$tag" '{name: $name, body: .body}' "$evidence/generated.json" > "$evidence/edit.json"
  gh api --method PATCH "repos/$repo/releases/$id" --input "$evidence/edit.json" > "$evidence/result.json"
fi
gh api "repos/$repo/releases/tags/$tag" > "$evidence/after.json"
jq -e --arg tag "$tag" --slurpfile generated "$evidence/generated.json" \
  '.name == $tag and .tag_name == $tag and .body == $generated[0].body' "$evidence/after.json" >/dev/null \
  || fail '게시 후 본문 확인 실패; 자동 재시도하지 말고 원격 상태를 확인하십시오'
if [[ "$mode" == edit ]]; then
  jq -e --slurpfile before "$evidence/before.json" '
    def retained: {id, tag_name, target_commitish, draft, prerelease, published_at, assets: [.assets[] | {id, name, digest}]};
    retained == ($before[0] | retained)' "$evidence/after.json" >/dev/null \
    || fail '게시 후 보존 속성 불일치; 원격 상태를 확인하십시오'
fi
echo "[release] 게시 결과 확인 완료: https://github.com/$repo/releases/tag/$tag"
