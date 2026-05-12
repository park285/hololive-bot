# Proposed script: check-current-docs-no-historical.sh

목적: `docs/current` 하위에 historical 상태 문서가 남아 있는지 검사합니다.

```bash
#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"
CURRENT_DIR="${ROOT_DIR}/docs/current"

tmp_hits="$(mktemp)"
trap 'rm -f "${tmp_hits}"' EXIT

grep -R -n -i   -e 'CLOSED / HISTORICAL'   -e 'historical'   -e '폐기 문서'   -e '과거 계획'   "${CURRENT_DIR}" --include='*.md'   | grep -v 'docs/current/README.md'   > "${tmp_hits}" || true

if [[ -s "${tmp_hits}" ]]; then
  echo "FAIL: historical marker found under docs/current" >&2
  cat "${tmp_hits}" >&2
  exit 1
fi

echo "OK: no historical markers under docs/current"
```

주의: 초기 도입 시에는 warning mode로 시작해도 됩니다.
