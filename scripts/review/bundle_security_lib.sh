# shellcheck shell=bash
# 공유 fixture/헬퍼 — bundle_*_security_test.sh에서 source 전용 (직접 실행하지 않음).

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
TMP_DIR="$(mktemp -d)"
trap 'rm -rf "${TMP_DIR}"' EXIT

failures=0

record_fail() {
  echo "[FAIL] $*" >&2
  failures=$((failures + 1))
}

pass() {
  echo "[PASS] $*"
}

report_results() {
  if (( failures > 0 )); then
    echo "[FAIL] bundle security tests failed: ${failures}" >&2
    exit 1
  fi
  echo "ok: bundle security tests passed"
}

setup_fixture() {
  local workdir="$1"

  mkdir -p \
    "${workdir}/scripts/review" \
    "${workdir}/scripts/architecture/lib" \
    "${workdir}/nested" \
    "${workdir}/.git/hooks-disabled"

  cp "${ROOT_DIR}/scripts/review/export-source-bundle.sh" "${workdir}/scripts/review/export-source-bundle.sh"
  cp "${ROOT_DIR}/scripts/review/verify-full-bundle.sh" "${workdir}/scripts/review/verify-full-bundle.sh"
  cp "${ROOT_DIR}/scripts/architecture/lib/git_guard.sh" "${workdir}/scripts/architecture/lib/git_guard.sh"
  chmod +x "${workdir}/scripts/review/export-source-bundle.sh" "${workdir}/scripts/review/verify-full-bundle.sh"

  git -C "${workdir}" init -q
  git -C "${workdir}" config user.email "codex@example.invalid"
  git -C "${workdir}" config user.name "Codex"
  git -C "${workdir}" config core.hooksPath "${workdir}/.git/hooks-disabled"

  printf 'fixture readme\n' >"${workdir}/README.md"
  printf 'tracked payload\n' >"${workdir}/nested/payload.txt"
  git -C "${workdir}" add \
    README.md \
    nested/payload.txt \
    scripts/architecture/lib/git_guard.sh \
    scripts/review/export-source-bundle.sh \
    scripts/review/verify-full-bundle.sh
  git -C "${workdir}" commit -q -m "fixture"

  printf 'local secret\n' >"${workdir}/foo.pem.bak"
  ln -s README.md "${workdir}/.env.secret"
}

make_unsafe_tar() {
  local archive="$1"
  local kind="$2"
  local absolute_target="${3:-/tmp/hololive-bundle-security-test-evil}"

  python3 - "${archive}" "${kind}" "${absolute_target}" <<'PY'
import io
import stat
import sys
import tarfile

archive, kind, absolute_target = sys.argv[1:4]

def add_regular(tf, name, data=b"payload\n", mode=0o644):
    info = tarfile.TarInfo(name)
    info.size = len(data)
    info.mode = mode
    tf.addfile(info, io.BytesIO(data))

with tarfile.open(archive, "w:gz") as tf:
    if kind == "dotdot":
        add_regular(tf, "../evil", b"evil\n")
    elif kind == "absolute":
        add_regular(tf, absolute_target, b"evil\n")
    elif kind == "symlink":
        add_regular(tf, "README.md", b"fixture readme\n")
        info = tarfile.TarInfo("symlink-entry")
        info.type = tarfile.SYMTYPE
        info.linkname = "README.md"
        info.mode = 0o777
        tf.addfile(info)
    elif kind == "hardlink":
        add_regular(tf, "README.md", b"fixture readme\n")
        info = tarfile.TarInfo("hardlink-entry")
        info.type = tarfile.LNKTYPE
        info.linkname = "README.md"
        info.mode = 0o644
        tf.addfile(info)
    elif kind == "device":
        info = tarfile.TarInfo("device-entry")
        info.type = tarfile.CHRTYPE
        info.mode = 0o600
        info.devmajor = 1
        info.devminor = 3
        tf.addfile(info)
    elif kind == "setuid":
        add_regular(tf, "setuid-entry", b"payload\n", stat.S_ISUID | 0o755)
    else:
        raise SystemExit(f"unknown kind: {kind}")
PY
}

expect_verify_rejects_before_extract() {
  local label="$1"
  local workdir="$2"
  local archive="$3"
  local expected="$4"
  local outside_path="${5:-}"
  local out_file="${TMP_DIR}/${label}.out"
  local err_file="${TMP_DIR}/${label}.err"
  local tmp_parent="${TMP_DIR}/${label}-tmp"

  mkdir -p "${tmp_parent}"
  if TMPDIR="${tmp_parent}" "${workdir}/scripts/review/verify-full-bundle.sh" "${archive}" >"${out_file}" 2>"${err_file}"; then
    cat "${out_file}" >&2
    cat "${err_file}" >&2
    record_fail "expected verifier rejection: ${label}"
    return
  fi

  if ! grep -Fq "${expected}" "${err_file}"; then
    cat "${out_file}" >&2
    cat "${err_file}" >&2
    record_fail "expected pre-extraction rejection message for ${label}: ${expected}"
    return
  fi

  if [[ -e "${tmp_parent}/evil" ]]; then
    record_fail "path traversal wrote outside extraction dir: ${label}"
    return
  fi
  if [[ -n "${outside_path}" && -e "${outside_path}" ]]; then
    record_fail "absolute path wrote outside extraction dir: ${label}"
    return
  fi

  pass "${label}"
}

assert_manifest_hashes_match_tar() {
  local archive="$1"

  python3 - "${archive}" <<'PY'
import hashlib
import io
import re
import sys
import tarfile

archive = sys.argv[1]
with tarfile.open(archive, "r:gz") as tf:
    members = tf.getmembers()
    names = [member.name for member in members]
    if "BUNDLE_MANIFEST.txt" not in names:
        raise SystemExit("manifest missing")
    regular = {
        member.name: hashlib.sha256(tf.extractfile(member).read()).hexdigest()
        for member in members
        if member.isfile() and member.name != "BUNDLE_MANIFEST.txt"
    }
    manifest_member = tf.getmember("BUNDLE_MANIFEST.txt")
    manifest = tf.extractfile(manifest_member).read().decode("utf-8")

lines = manifest.splitlines()
try:
    files_index = lines.index("files:")
except ValueError as exc:
    raise SystemExit("manifest files section missing") from exc

fields = {}
for line in lines[:files_index]:
    if ": " in line:
        key, value = line.split(": ", 1)
        fields[key] = value

if fields.get("format") != "hololive-review-bundle-v1":
    raise SystemExit("manifest format mismatch")
if int(fields.get("file_count", "-1")) != len(regular):
    raise SystemExit("manifest file_count mismatch")

manifest_hashes = {}
for line in lines[files_index + 1:]:
    if not line:
        continue
    match = re.fullmatch(r"([0-9a-f]{64})  (.+)", line)
    if not match:
        raise SystemExit(f"invalid manifest file line: {line}")
    digest, path = match.groups()
    manifest_hashes[path] = digest

if manifest_hashes != regular:
    raise SystemExit("manifest hashes differ from tar payload")
PY
}

make_tampered_bundle_with_matching_manifest() {
  local workdir="$1"
  local archive="$2"
  local trusted_manifest="${3:-}"

  python3 - "${workdir}" "${archive}" "${trusted_manifest}" <<'PY'
import hashlib
import io
import subprocess
import sys
import tarfile
from pathlib import Path

workdir = Path(sys.argv[1])
archive = Path(sys.argv[2])
trusted_manifest = Path(sys.argv[3]) if sys.argv[3] else None
tracked_paths = subprocess.check_output(
    ["git", "-C", str(workdir), "ls-files", "-z", "--cached"]
).rstrip(b"\0").split(b"\0")
paths = sorted(path.decode("utf-8") for path in tracked_paths if path)
target = "nested/payload.txt"
payloads = {}
for path in paths:
    data = (workdir / path).read_bytes()
    if path == target:
        data = b"tampered payload\n"
    payloads[path] = data

bundle_excludes = [
    ".git",
    ".worktrees",
    ".tasklists",
    ".runlogs",
    ".codex",
    ".claude",
    ".serena",
    ".gemini",
    "artifacts",
    "backups",
    "data",
    "logs",
    "runtime-config",
    ".env",
    ".env.*",
    "**/.env",
    "**/.env.*",
    "*.key",
    "*.key.*",
    "*.pem",
    "*.pem.*",
    "node_modules",
    "dist",
    "coverage",
    "*.tar.gz",
    "BUNDLE_MANIFEST.txt",
    ".idea",
    ".vscode",
    ".omc",
]
manifest_lines = [
    "format: hololive-review-bundle-v1",
    "mode: source",
    "policy: tracked-only",
    "tracked_only: true",
    "allowlist_file: <none>",
    "generated_at: 2026-06-05T00:00:00Z",
    "branch: " + subprocess.check_output(
        ["git", "-C", str(workdir), "rev-parse", "--abbrev-ref", "HEAD"], text=True
    ).strip(),
    "commit: " + subprocess.check_output(
        ["git", "-C", str(workdir), "rev-parse", "HEAD"], text=True
    ).strip(),
    f"file_count: {len(payloads)}",
    "excluded_patterns: " + ",".join(bundle_excludes),
    "files:",
]
for path, data in payloads.items():
    manifest_lines.append(f"{hashlib.sha256(data).hexdigest()}  {path}")
manifest_data = ("\n".join(manifest_lines) + "\n").encode("utf-8")

with tarfile.open(archive, "w:gz") as tf:
    for path, data in payloads.items():
        info = tarfile.TarInfo(path)
        info.mode = 0o644
        info.size = len(data)
        tf.addfile(info, io.BytesIO(data))

    info = tarfile.TarInfo("BUNDLE_MANIFEST.txt")
    info.mode = 0o644
    info.size = len(manifest_data)
    tf.addfile(info, io.BytesIO(manifest_data))

if trusted_manifest is not None:
    trusted_manifest.write_bytes(manifest_data)
PY
}
