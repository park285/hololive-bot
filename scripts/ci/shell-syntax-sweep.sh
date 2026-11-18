#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "${ROOT_DIR}"

while IFS= read -r -d '' script; do
    [[ -f "${script}" ]] || continue
    if [[ "$(head -n 1 "${script}")" =~ ^#![[:space:]]*(/usr)?/bin/(env[[:space:]]+)?(sh|dash)([[:space:]]|$) ]]; then
        sh -n "${script}"
    else
        bash -n "${script}"
    fi
done < <(git ls-files -z --cached --others --exclude-standard -- '*.sh')
