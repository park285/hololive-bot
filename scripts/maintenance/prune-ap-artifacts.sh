#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="${REPO_ROOT:-$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd -P)}"
readonly REPO_ROOT

source "${REPO_ROOT}/scripts/deploy/lib/ap-host-native-release-path.sh"

ARTIFACT_ROOT="${ARTIFACT_ROOT:-${REPO_ROOT}/artifacts/ap-host-native}"
keep=3
apply=false

usage() {
  cat <<'EOF'
Usage: bash scripts/maintenance/prune-ap-artifacts.sh [--apply] [--keep N]

ap-host-native-deploy.sh가 남기는 빌드 페이로드를 최신 N개만 남기고 지웁니다.
해당 스크립트는 배포마다 artifacts/ap-host-native/<release-id>를 새로 만들고
스스로 회수하지 않아, 배포를 반복하면 무한히 쌓입니다.

  --apply     실제로 삭제합니다. 생략하면 dry-run으로 대상만 출력합니다.
  --keep N    남길 최신 페이로드 개수 (기본 3, 0이면 전부 삭제).

삭제 대상은 mtime 최신순으로 고릅니다. 각 후보는 안전한 단일 경로 요소인지
(native_release_id_validate), ARTIFACT_ROOT 안에 있는지, git이 무시하는 경로인지
모두 통과해야 삭제합니다. 하나라도 어긋나면 그 후보를 건너뛰고 계속합니다.
EOF
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --apply) apply=true ;;
    --keep)
      shift
      keep="${1:?--keep requires a count}"
      ;;
    -h | --help)
      usage
      exit 0
      ;;
    *)
      printf 'unknown option: %s\n' "$1" >&2
      usage >&2
      exit 2
      ;;
  esac
  shift
done

if [[ ! "$keep" =~ ^[0-9]+$ ]]; then
  printf -- '--keep must be a non-negative integer: %s\n' "$keep" >&2
  exit 2
fi

if [[ ! -d "$ARTIFACT_ROOT" ]]; then
  printf 'nothing to prune: %s does not exist\n' "$ARTIFACT_ROOT"
  exit 0
fi

artifact_root_real="$(realpath -e -- "$ARTIFACT_ROOT")"
repo_artifacts_real="$(realpath -e -- "${REPO_ROOT}/artifacts")"
case "$artifact_root_real" in
  "$repo_artifacts_real"/*) ;;
  *)
    printf 'refusing to prune outside the repository artifacts tree: %s\n' "$artifact_root_real" >&2
    exit 1
    ;;
esac

declare -a ordered=()
while IFS= read -r line; do
  ordered+=("${line#* }")
done < <(find "$artifact_root_real" -mindepth 1 -maxdepth 1 -printf '%T@ %f\n' | sort -rn)

total="${#ordered[@]}"
printf 'artifact root: %s\n' "$artifact_root_real"
printf 'payloads: %s (keep newest %s)\n' "$total" "$keep"

if (( total <= keep )); then
  printf 'nothing to prune\n'
  exit 0
fi

reclaimed=0
skipped=0
for name in "${ordered[@]:keep}"; do
  candidate="$artifact_root_real/$name"

  if ! native_release_id_validate "$name" >/dev/null 2>&1; then
    printf '  SKIP (unsafe name)      %s\n' "$name" >&2
    skipped=$((skipped + 1))
    continue
  fi

  candidate_real="$(realpath -m -- "$candidate")"
  case "$candidate_real" in
    "$artifact_root_real"/*) ;;
    *)
      printf '  SKIP (escapes root)     %s\n' "$name" >&2
      skipped=$((skipped + 1))
      continue
      ;;
  esac

  # 추적 중인 파일은 절대 지우지 않는다. artifacts/*는 gitignore 대상이지만
  # artifacts/architecture/처럼 되살린 예외가 있어, 경로마다 직접 확인한다.
  if ! git -C "$REPO_ROOT" check-ignore -q -- "$candidate_real"; then
    printf '  SKIP (not gitignored)   %s\n' "$name" >&2
    skipped=$((skipped + 1))
    continue
  fi

  size_kib="$(du -sk -- "$candidate_real" | cut -f1)"
  if [[ "$apply" == true ]]; then
    rm -rf -- "$candidate_real"
    printf '  removed  %8s MiB  %s\n' "$((size_kib / 1024))" "$name"
  else
    printf '  would remove  %8s MiB  %s\n' "$((size_kib / 1024))" "$name"
  fi
  reclaimed=$((reclaimed + size_kib))
done

if [[ "$apply" == true ]]; then
  printf 'reclaimed %s MiB\n' "$((reclaimed / 1024))"
else
  printf 'dry-run: %s MiB would be reclaimed (pass --apply to delete)\n' "$((reclaimed / 1024))"
fi
if (( skipped > 0 )); then
  printf 'skipped %s payload(s); see messages above\n' "$skipped" >&2
fi
