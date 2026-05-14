#!/usr/bin/env bash
set -euo pipefail

changed="$(
  {
    git diff --name-only HEAD 2>/dev/null || git diff --name-only
    git ls-files --others --exclude-standard 2>/dev/null || true
  } | sort -u
)"
if echo "$changed" | grep -E '^(hololive/hololive-admin-api/|admin-dashboard/)'; then
  echo "admin scope files changed; this refactor must not touch admin-api/admin-dashboard" >&2
  exit 1
fi

echo "ok: no admin scope changes"
