#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"
GATE="${SCRIPT_DIR}/check-youtube-collector-hardening-contract.sh"

TMP_ROOT="$(mktemp -d)"
trap 'rm -rf "${TMP_ROOT}"' EXIT

LAST_STATUS=0
LAST_OUTPUT=""
PASSED=0

run_gate() {
  local root="$1"
  local rules="$2"
  set +e
  LAST_OUTPUT="$(bash "${GATE}" --root "${root}" --rules "${rules}" 2>&1)"
  LAST_STATUS=$?
  set -e
}

assert_success() {
  local name="$1"
  if [[ "${LAST_STATUS}" -ne 0 ]]; then
    printf 'not ok - %s\n%s\n' "${name}" "${LAST_OUTPUT}" >&2
    exit 1
  fi
  PASSED=$((PASSED + 1))
  printf 'ok - %s\n' "${name}"
}

assert_failure() {
  local name="$1"
  if [[ "${LAST_STATUS}" -eq 0 ]]; then
    printf 'not ok - %s unexpectedly succeeded\n%s\n' "${name}" "${LAST_OUTPUT}" >&2
    exit 1
  fi
  PASSED=$((PASSED + 1))
  printf 'ok - %s\n' "${name}"
}

write_rules() {
  local dest="$1"
  shift
  printf '%s\n' "$@" >"${dest}"
}

case_path_glob_is_not_global() {
  local dir="${TMP_ROOT}/path-glob"
  mkdir -p "${dir}/src" "${dir}/other"
  printf 'package main\nvar x = "-pgo=off"\n' >"${dir}/other/main.go"
  printf 'package main\n' >"${dir}/src/main.go"
  write_rules "${dir}/rules.tsv" \
    $'HC-TEST\ttrue\tPR-00\tsrc/*.go\tforbidden\t-pgo=off\t0\t0\t'
  run_gate "${dir}" "${dir}/rules.tsv"
  assert_success "path glob does not count files outside the glob"
}

case_expected_counts() {
  local dir="${TMP_ROOT}/counts"
  mkdir -p "${dir}/src"
  printf 'alpha sonic\nbeta sonic\n' >"${dir}/src/flags.txt"
  write_rules "${dir}/rules.tsv" \
    $'HC-TEST\ttrue\tPR-00\tsrc/flags.txt\trequired\tsonic\t2\t2\t'
  run_gate "${dir}" "${dir}/rules.tsv"
  assert_success "expected count range accepts an exact match"

  write_rules "${dir}/rules.tsv" \
    $'HC-TEST\ttrue\tPR-00\tsrc/flags.txt\trequired\tsonic\t3\t3\t'
  run_gate "${dir}" "${dir}/rules.tsv"
  assert_failure "expected count range rejects a shortfall"
}

case_comments_do_not_satisfy_forbidden_rule() {
  local dir="${TMP_ROOT}/comments"
  mkdir -p "${dir}/src"
  cat >"${dir}/src/main.go" <<'EOF'
package main

// -pgo=off -tags sonic
func main() {}
EOF
  write_rules "${dir}/rules.tsv" \
    $'HC-TEST\ttrue\tPR-00\tsrc/main.go\tforbidden\t-pgo=off\t0\t0\t'
  run_gate "${dir}" "${dir}/rules.tsv"
  assert_success "comments do not satisfy a forbidden production rule"

  printf 'package main\nvar flags = "-pgo=off"\n' >"${dir}/src/main.go"
  run_gate "${dir}" "${dir}/rules.tsv"
  assert_failure "live source text still satisfies a forbidden production rule"
}

case_allowlist_excludes_fixture() {
  local dir="${TMP_ROOT}/allowlist"
  mkdir -p "${dir}/src" "${dir}/testdata"
  printf 'forbidden-token\n' >"${dir}/src/prod.go"
  printf 'forbidden-token\n' >"${dir}/testdata/fixture.go"
  printf 'src/prod.go\n' >"${dir}/allow.txt"
  write_rules "${dir}/rules.tsv" \
    $'HC-TEST\ttrue\tPR-00\t**/*.go\tforbidden\tforbidden-token\t0\t0\tallow.txt'
  run_gate "${dir}" "${dir}/rules.tsv"
  assert_failure "non-allow-listed production path still counts"

  printf 'testdata/fixture.go\n' >"${dir}/allow.txt"
  printf 'package main\n' >"${dir}/src/prod.go"
  run_gate "${dir}" "${dir}/rules.tsv"
  assert_success "fixture allow-list excludes matching testdata"
}

case_canonical_table_has_required_ids() {
  local ids
  ids="$(awk -F'\t' 'NF && $1 !~ /^#/ { print $1 }' "${SCRIPT_DIR}/youtube-collector-hardening-contract.tsv" | sort -u | tr '\n' ' ')"
  local expected="HC-001 HC-002 HC-003 HC-004 HC-005 HC-006 HC-007 HC-008 HC-009 HC-010 HC-011 HC-012 HC-013 HC-014 HC-015 "
  if [[ "${ids}" != "${expected}" ]]; then
    printf 'not ok - canonical rule IDs differ\nexpected: %s\nactual: %s\n' "${expected}" "${ids}" >&2
    exit 1
  fi
  PASSED=$((PASSED + 1))
  printf 'ok - canonical rule table registers HC-001..HC-015\n'
}

case_valkey_phrase_globs_exclude_rollback() {
  if awk -F'\t' 'NF && $1 !~ /^#/ && $1 == "HC-015" && $4 ~ /rollback\.md/' "${SCRIPT_DIR}/youtube-collector-hardening-contract.tsv" | grep -q .; then
    printf 'not ok - HC-015 glob must not scan rollback.md\n' >&2
    exit 1
  fi
  PASSED=$((PASSED + 1))
  printf 'ok - HC-015 Valkey phrase globs exclude rollback.md\n'
}

case_hc015_forbids_optional_optimization_on_collector_docs() {
  local rows
  rows="$(awk -F'\t' 'NF && $1 !~ /^#/ && $1 == "HC-015" && $6 == "optional optimization" { print $4 }' "${SCRIPT_DIR}/youtube-collector-hardening-contract.tsv" | sort | tr '\n' ' ')"
  local expected="docs/current/runbooks/youtube-collector.md docs/current/services/youtube-collector.md "
  if [[ "${rows}" != "${expected}" ]]; then
    printf 'not ok - HC-015 optional optimization globs differ\nexpected: %s\nactual: %s\n' "${expected}" "${rows}" >&2
    exit 1
  fi
  PASSED=$((PASSED + 1))
  printf 'ok - HC-015 forbids optional optimization on collector service and runbook docs\n'
}

case_path_glob_is_not_global
case_expected_counts
case_comments_do_not_satisfy_forbidden_rule
case_allowlist_excludes_fixture
case_canonical_table_has_required_ids
case_valkey_phrase_globs_exclude_rollback
case_hc015_forbids_optional_optimization_on_collector_docs

printf 'ok - %s hardening-contract parser checks passed\n' "${PASSED}"
