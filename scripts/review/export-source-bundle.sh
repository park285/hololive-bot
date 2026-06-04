#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
STAMP="$(date -u +%Y%m%dT%H%M%SZ)"
OUT_DIR="${1:-${ROOT_DIR}/artifacts/review}"
OUT_FILE="${OUT_DIR}/hololive-bot-source-${STAMP}.tar.gz"

mkdir -p "${OUT_DIR}"

tar \
  --exclude-vcs \
  --exclude='.worktrees' \
  --exclude='.tasklists' \
  --exclude='.runlogs' \
  --exclude='.codex' \
  --exclude='.claude' \
  --exclude='.serena' \
  --exclude='.gemini' \
  --exclude='artifacts' \
  --exclude='backups' \
  --exclude='data' \
  --exclude='logs' \
  --exclude='runtime-config' \
  --exclude='.env' \
  --exclude='.env.*' \
  --exclude='**/.env' \
  --exclude='**/.env.*' \
  --exclude='*.key' \
  --exclude='*.pem' \
  --exclude='**/node_modules' \
  --exclude='**/dist' \
  --exclude='**/coverage' \
  --exclude='*.tar.gz' \
  --exclude='BUNDLE_MANIFEST.txt' \
  -czf "${OUT_FILE}" \
  -C "${ROOT_DIR}" .

echo "${OUT_FILE}"
