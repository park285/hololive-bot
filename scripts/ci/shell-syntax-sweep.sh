#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "${ROOT_DIR}"

git ls-files -z --cached --others --exclude-standard -- '*.sh' |
while IFS= read -r -d '' script; do
    [[ -f "${script}" ]] || continue
    IFS= read -r shebang <"${script}" || [[ -n "${shebang}" || ! -s "${script}" ]]
    if [[ "${shebang}" =~ ^#![[:space:]]*(/usr)?/bin/(env[[:space:]]+)?(sh|dash)([[:space:]]|$) ]]; then
        sh -n "./${script}"
    else
        bash -n "./${script}"
    fi
done