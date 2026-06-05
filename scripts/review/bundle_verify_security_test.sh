#!/usr/bin/env bash
set -euo pipefail

# shellcheck source=scripts/review/bundle_security_lib.sh
. "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/bundle_security_lib.sh"

test_verifier_rejects_tampered_payload_with_matching_manifest() {
  local workdir="${TMP_DIR}/tampered-fixture"
  local archive="${TMP_DIR}/tampered-matching-manifest.tar.gz"
  local trusted_manifest="${TMP_DIR}/tampered-detached-manifest.txt"
  local trusted_out_file="${TMP_DIR}/tampered-trusted-verify.out"
  local trusted_err_file="${TMP_DIR}/tampered-trusted-verify.err"
  local out_file="${TMP_DIR}/tampered-verify.out"
  local err_file="${TMP_DIR}/tampered-verify.err"
  setup_fixture "${workdir}"
  make_tampered_bundle_with_matching_manifest "${workdir}" "${archive}" "${trusted_manifest}"

  if "${workdir}/scripts/review/verify-full-bundle.sh" "${archive}" >"${out_file}" 2>"${err_file}"; then
    cat "${out_file}" >&2
    cat "${err_file}" >&2
    record_fail "verifier should reject internally consistent tampered bundle"
    return
  fi
  if ! grep -Fq "FAIL: bundle file contents differ from current checkout" "${err_file}"; then
    cat "${out_file}" >&2
    cat "${err_file}" >&2
    record_fail "expected current-checkout authenticity rejection for tampered bundle"
    return
  fi
  if ! "${workdir}/scripts/review/verify-full-bundle.sh" "${archive}" "${trusted_manifest}" >"${trusted_out_file}" 2>"${trusted_err_file}"; then
    cat "${trusted_out_file}" >&2
    cat "${trusted_err_file}" >&2
    record_fail "verifier should accept tampered bundle only with trusted detached manifest"
    return
  fi

  pass "verifier rejects internally consistent tampered bundle"
}

test_verifier_rejects_malicious_members() {
  local workdir="${TMP_DIR}/verify-fixture"
  setup_fixture "${workdir}"

  local archive
  archive="${TMP_DIR}/dotdot.tar.gz"
  make_unsafe_tar "${archive}" "dotdot"
  expect_verify_rejects_before_extract \
    "verifier rejects ../evil before extraction" \
    "${workdir}" \
    "${archive}" \
    "FAIL: unsafe tar member path before extraction: ../evil"

  local absolute_parent="${TMP_DIR}/absolute-parent"
  mkdir -p "${absolute_parent}"
  archive="${TMP_DIR}/absolute.tar.gz"
  make_unsafe_tar "${archive}" "absolute" "${absolute_parent}/evil"
  expect_verify_rejects_before_extract \
    "verifier rejects absolute path before extraction" \
    "${workdir}" \
    "${archive}" \
    "FAIL: unsafe tar member path before extraction: ${absolute_parent}/evil" \
    "${absolute_parent}/evil"

  archive="${TMP_DIR}/symlink.tar.gz"
  make_unsafe_tar "${archive}" "symlink"
  expect_verify_rejects_before_extract \
    "verifier rejects symlink member before extraction" \
    "${workdir}" \
    "${archive}" \
    "FAIL: unsafe tar member type before extraction: symlink-entry"

  archive="${TMP_DIR}/hardlink.tar.gz"
  make_unsafe_tar "${archive}" "hardlink"
  expect_verify_rejects_before_extract \
    "verifier rejects hardlink member before extraction" \
    "${workdir}" \
    "${archive}" \
    "FAIL: unsafe tar member type before extraction: hardlink-entry"

  archive="${TMP_DIR}/device.tar.gz"
  make_unsafe_tar "${archive}" "device"
  expect_verify_rejects_before_extract \
    "verifier rejects device member before extraction" \
    "${workdir}" \
    "${archive}" \
    "FAIL: unsafe tar member type before extraction: device-entry"

  archive="${TMP_DIR}/setuid.tar.gz"
  make_unsafe_tar "${archive}" "setuid"
  expect_verify_rejects_before_extract \
    "verifier rejects setuid member before extraction" \
    "${workdir}" \
    "${archive}" \
    "FAIL: unsafe tar member mode before extraction: setuid-entry"
}

test_verifier_rejects_malicious_members
test_verifier_rejects_tampered_payload_with_matching_manifest

report_results
