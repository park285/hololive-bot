#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"

git -C "${ROOT_DIR}" ls-files --cached -z | (
  status=0
  while IFS= read -r -d '' path; do
    case "${path}" in
      scripts/ci/nilaway-models/models.patch|deploy/images/pgbouncer/unreported-startup-parameters.patch|logs/.gitkeep|runtime-config/.gitkeep|runtime-config/README.md|runtime-config/*.example)
        continue ;;
      logs/*|data/*|backups/*|.review-bundles/*|runtime-config/*|.env|.env.local|.env.*.local|\
      .env.osaka|.env.seoul|.env.main-ap|.env.prod|.env.production|.env.staging|\
      */.env.osaka|*/.env.seoul|*/.env.main-ap|*/.env.prod|*/.env.production|*/.env.staging|\
      *.key|*.pem|.worktrees/*|.tasklists/*|.runlogs/*|.codex/*|.claude/*|.serena/*|.gemini/*|\
      .idea/*|*/.idea/*|.vscode/*|*/.vscode/*|.omc/*|*/.omc/*|\
      BUNDLE_MANIFEST.txt|*.zip|*.tar|*.tar.gz|*.patch|*.diff|*_artifact.*|*.orig|*.rej)
        printf 'FAIL: tracked local artifact: %q\n' "${path}" >&2
        status=1 ;;
    esac
  done
  exit "${status}"
)

echo "OK: no tracked local artifacts"