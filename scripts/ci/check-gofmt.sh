#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "${ROOT_DIR}"

unformatted="$(
    git ls-files -z --cached --others --exclude-standard -- '*.go' |
        while IFS= read -r -d '' file; do
            if [[ -f "${file}" ]]; then
                printf './%s\0' "${file}"
            fi
        done | xargs -0 -r gofmt -l
)"
if [[ -n "${unformatted}" ]]; then
    echo "gofmt required for:" >&2
    echo "${unformatted}" >&2
    exit 1
fi
