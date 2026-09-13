#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
filter="$script_dir/disabled-bake-attestations.jq"

check() {
  local expected="$1" name="$2" document="$3" actual=reject
  if jq -e -f "$filter" <<<"$document" >/dev/null 2>&1; then
    actual=accept
  fi
  if [[ "$actual" != "$expected" ]]; then
    echo "FAIL: $name: expected $expected, got $actual" >&2
    exit 1
  fi
  echo "PASS: $name"
}

check accept 'Compose 5 explicit disabled attestations' \
  '{"target":{"api":{"attest":["type=provenance,disabled=true","type=sbom,disabled=true"]}}}'
check accept 'omitted or empty attestations' \
  '{"target":{"api":{},"worker":{"attest":[]}}}'
check reject 'enabled provenance among disabled targets' \
  '{"target":{"api":{"attest":["type=sbom,disabled=true"]},"worker":{"attest":["type=provenance,mode=max"]}}}'
check reject 'enabled sbom' '{"target":{"api":{"attest":["type=sbom"]}}}'
check reject 'explicit enabled flag' '{"target":{"api":{"attest":["type=sbom,disabled=false"]}}}'
check reject 'unknown attestation' '{"target":{"api":{"attest":["type=unknown,disabled=true"]}}}'
check reject 'invalid attestation shape' '{"target":{"api":{"attest":false}}}'
check reject 'missing targets' '{}'
check reject 'empty targets' '{"target":{}}'
check reject 'invalid JSON' '{'
