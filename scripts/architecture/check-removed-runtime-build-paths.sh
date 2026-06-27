#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"
cd "${ROOT_DIR}"

retired='hololive/hololive-(kakao-bot-go|admin-api|llm-sched)'

mapfile -t files < <(
  find hololive deploy/compose -type f \( -name 'Dockerfile' -o -name '*.yml' -o -name '*.yaml' \) 2>/dev/null | sort
)

if [[ ${#files[@]} -eq 0 ]]; then
  echo "OK: no build/deploy files to scan"
  exit 0
fi

matches="$(grep -REn "${retired}" "${files[@]}" 2>/dev/null || true)"

if [[ -n "${matches}" ]]; then
  echo "FAIL: removed runtime directory paths referenced in build/deploy files" >&2
  echo "${matches}" >&2
  exit 1
fi

echo "OK: no removed runtime directory paths in Dockerfiles or compose files"
