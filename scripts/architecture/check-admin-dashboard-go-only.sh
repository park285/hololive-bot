#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "${ROOT_DIR}"

forbidden_paths=()
while IFS= read -r path; do
  forbidden_paths+=("${path}")
done < <(find admin-dashboard/backend -type f \( -name '*.rs' -o -name 'Cargo.toml' -o -name 'Cargo.lock' \) 2>/dev/null || true)

if (( ${#forbidden_paths[@]} > 0 )); then
  printf 'admin-dashboard backend still contains Rust artifacts:\n' >&2
  printf ' - %s\n' "${forbidden_paths[@]}" >&2
  exit 1
fi

if grep -RInE '\b(cargo|rustc|rust-embed|axum|tokio|bollard)\b' admin-dashboard/Dockerfile admin-dashboard/backend 2>/dev/null; then
  echo 'admin-dashboard backend still references Rust-only tooling or crates' >&2
  exit 1
fi

if ! grep -q './admin-dashboard/backend' go.work; then
  echo 'go.work must include ./admin-dashboard/backend' >&2
  exit 1
fi

source scripts/ci/go-workspace-modules.sh
if ! printf '%s\n' "${GO_WORKSPACE_MODULES[@]}" | grep -Fxq 'admin-dashboard/backend'; then
  echo 'Go workspace CI module list must include admin-dashboard/backend' >&2
  exit 1
fi

echo 'ok: admin-dashboard backend is Go-only and CI-visible'
