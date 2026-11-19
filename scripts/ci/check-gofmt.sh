#!/usr/bin/env bash
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
