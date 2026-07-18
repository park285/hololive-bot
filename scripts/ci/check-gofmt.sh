#!/usr/bin/env bash
# tracked+untracked(비-ignore) 전체를 훑어야 CI checkout(=tracked 전체)과 로컬
# 작업 트리 스코프를 동시에 포섭한다 — 목록을 좁히면 한쪽 게이트가 약해진다.
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "${ROOT_DIR}"

files=()
while IFS= read -r file; do
    if [[ -f "${file}" ]]; then
        files+=("${file}")
    fi
done < <(git ls-files --cached --others --exclude-standard '*.go')

if (( ${#files[@]} == 0 )); then
    exit 0
fi

unformatted="$(gofmt -l "${files[@]}")"
if [[ -n "${unformatted}" ]]; then
    echo "gofmt required for:" >&2
    echo "${unformatted}" >&2
    exit 1
fi
