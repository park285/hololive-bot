#!/usr/bin/env bash
set -euo pipefail

# shellcheck source=scripts/review/bundle_security_lib.sh
. "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/bundle_security_lib.sh"

test_exporter_tracked_only_and_manifest() {
  local workdir="${TMP_DIR}/export-fixture"
  local out_dir="${TMP_DIR}/exported"
  local out_file="${TMP_DIR}/export.out"
  local err_file="${TMP_DIR}/export.err"
  setup_fixture "${workdir}"

  if ! "${workdir}/scripts/review/export-source-bundle.sh" "${out_dir}" >"${out_file}" 2>"${err_file}"; then
    cat "${out_file}" >&2
    cat "${err_file}" >&2
    record_fail "source bundle export should succeed"
    return
  fi

  local bundle
  bundle="$(tail -n 1 "${out_file}")"
  if [[ ! -f "${bundle}" ]]; then
    cat "${out_file}" >&2
    cat "${err_file}" >&2
    record_fail "exporter did not print a bundle path"
    return
  fi

  if tar -tzf "${bundle}" | sed 's#^\./##' | grep -Fxq "foo.pem.bak"; then
    record_fail "exporter included untracked foo.pem.bak"
    return
  fi
  if tar -tzf "${bundle}" | sed 's#^\./##' | grep -Fxq ".env.secret"; then
    record_fail "exporter included untracked .env.secret symlink"
    return
  fi
  if ! tar -tzf "${bundle}" | sed 's#^\./##' | grep -Fxq "BUNDLE_MANIFEST.txt"; then
    record_fail "exporter bundle missing manifest"
    return
  fi
  if ! grep -Fq "OK: full bundle matches manifest" "${out_file}"; then
    cat "${out_file}" >&2
    record_fail "exporter did not run verifier post-step"
    return
  fi

  if ! "${workdir}/scripts/review/verify-full-bundle.sh" "${bundle}" >/dev/null 2>"${TMP_DIR}/verify-clean.err"; then
    cat "${TMP_DIR}/verify-clean.err" >&2
    record_fail "verifier should accept clean exported bundle"
    return
  fi
  if ! assert_manifest_hashes_match_tar "${bundle}"; then
    record_fail "manifest hashes should match exported tar"
    return
  fi

  printf 'allowed local file\n' >"${workdir}/local-note.txt"
  printf 'local-note.txt\n' >"${TMP_DIR}/allowlist.txt"
  if ! "${workdir}/scripts/review/export-source-bundle.sh" "${TMP_DIR}/allowlist-exported" "${TMP_DIR}/allowlist.txt" >"${TMP_DIR}/allowlist-export.out" 2>"${TMP_DIR}/allowlist-export.err"; then
    cat "${TMP_DIR}/allowlist-export.out" >&2
    cat "${TMP_DIR}/allowlist-export.err" >&2
    record_fail "source bundle export with allowlist should succeed"
    return
  fi

  local allowlist_bundle
  allowlist_bundle="$(tail -n 1 "${TMP_DIR}/allowlist-export.out")"
  if ! tar -tzf "${allowlist_bundle}" | sed 's#^\./##' | grep -Fxq "local-note.txt"; then
    record_fail "allowlisted untracked file missing from bundle"
    return
  fi
  if ! tar -xOf "${allowlist_bundle}" BUNDLE_MANIFEST.txt | grep -Fxq "policy: tracked-plus-allowlist"; then
    record_fail "allowlist export policy missing from manifest"
    return
  fi
  if ! assert_manifest_hashes_match_tar "${allowlist_bundle}"; then
    record_fail "allowlist manifest hashes should match exported tar"
    return
  fi

  pass "exporter emits tracked-only bundle with matching manifest"
}

test_exporter_rejects_tracked_symlink() {
  local workdir="${TMP_DIR}/symlink-export-fixture"
  local out_file="${TMP_DIR}/symlink-export.out"
  local err_file="${TMP_DIR}/symlink-export.err"
  setup_fixture "${workdir}"

  ln -s README.md "${workdir}/tracked-link"
  git -C "${workdir}" add tracked-link
  git -C "${workdir}" commit -q -m "add tracked symlink"

  if "${workdir}/scripts/review/export-source-bundle.sh" "${TMP_DIR}/symlink-exported" >"${out_file}" 2>"${err_file}"; then
    cat "${out_file}" >&2
    cat "${err_file}" >&2
    record_fail "exporter should reject tracked symlink"
    return
  fi
  if ! grep -Fq "FAIL: unsafe source bundle file type: tracked-link" "${err_file}"; then
    cat "${out_file}" >&2
    cat "${err_file}" >&2
    record_fail "expected tracked symlink rejection"
    return
  fi

  pass "exporter rejects tracked symlink"
}

test_exporter_rejects_broken_tracked_symlink() {
  local workdir="${TMP_DIR}/broken-symlink-export-fixture"
  local out_file="${TMP_DIR}/broken-symlink-export.out"
  local err_file="${TMP_DIR}/broken-symlink-export.err"
  setup_fixture "${workdir}"

  ln -s missing-target "${workdir}/broken-tracked-link"
  git -C "${workdir}" add broken-tracked-link
  git -C "${workdir}" commit -q -m "add broken tracked symlink"

  if "${workdir}/scripts/review/export-source-bundle.sh" "${TMP_DIR}/broken-symlink-exported" >"${out_file}" 2>"${err_file}"; then
    cat "${out_file}" >&2
    cat "${err_file}" >&2
    record_fail "exporter should reject broken tracked symlink"
    return
  fi
  if ! grep -Fq "FAIL: unsafe source bundle file type: broken-tracked-link" "${err_file}"; then
    cat "${out_file}" >&2
    cat "${err_file}" >&2
    record_fail "expected broken tracked symlink rejection"
    return
  fi

  pass "exporter rejects broken tracked symlink"
}

test_exporter_tracked_only_and_manifest
test_exporter_rejects_tracked_symlink
test_exporter_rejects_broken_tracked_symlink

report_results
