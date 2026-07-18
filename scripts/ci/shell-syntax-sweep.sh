#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "${ROOT_DIR}"

while IFS= read -r script; do
    bash -n "${script}"
done < <(find scripts -type f -name '*.sh' | sort)
