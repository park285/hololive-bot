#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"
if (( $# < 1 || $# > 2 )); then
  echo "usage: verify-full-bundle.sh <bundle.tar.gz> [trusted_manifest_or_checksum_file]" >&2
  exit 2
fi
ARCHIVE_PATH="$1"
TRUSTED_REFERENCE_PATH="${2:-}"
TMP_DIR="$(mktemp -d)"
MEMBER_LIST="$(mktemp)"

cleanup() {
  rm -rf "${TMP_DIR}"
  rm -f "${MEMBER_LIST}"
}
trap cleanup EXIT

source "${ROOT_DIR}/scripts/architecture/lib/git_guard.sh"
require_git_checkout "${ROOT_DIR}"

python3 - "${ARCHIVE_PATH}" "${MEMBER_LIST}" <<'PY'
import stat
import sys
import tarfile

archive_path, member_list_path = sys.argv[1:3]

def fail(message: str) -> None:
    print(f"FAIL: {message}", file=sys.stderr)
    raise SystemExit(1)

def validate_path(path: str) -> None:
    if not path or path.startswith("/"):
        fail(f"unsafe tar member path before extraction: {path}")
    if "\n" in path or "\r" in path:
        fail(f"unsafe tar member path before extraction: {path}")
    parts = path.split("/")
    if any(part in ("", ".", "..") for part in parts):
        fail(f"unsafe tar member path before extraction: {path}")

try:
    with tarfile.open(archive_path, "r:gz") as archive:
        seen = set()
        with open(member_list_path, "w", encoding="utf-8", newline="\n") as member_list:
            for member in archive.getmembers():
                validate_path(member.name)
                if member.name in seen:
                    fail(f"duplicate tar member before extraction: {member.name}")
                seen.add(member.name)

                if member.mode & stat.S_ISUID:
                    fail(f"unsafe tar member mode before extraction: {member.name}")
                if not member.isfile():
                    fail(f"unsafe tar member type before extraction: {member.name}")

                member_list.write(f"{member.name}\n")
except tarfile.TarError as exc:
    fail(f"cannot inspect tar before extraction: {exc}")
PY

tar \
  --extract \
  --gzip \
  --file "${ARCHIVE_PATH}" \
  --directory "${TMP_DIR}" \
  --no-same-owner \
  --no-same-permissions

python3 - "${TMP_DIR}" "${MEMBER_LIST}" "${ROOT_DIR}" "${TRUSTED_REFERENCE_PATH}" <<'PY'
from __future__ import annotations

import fnmatch
import hashlib
import subprocess
import sys
from pathlib import Path

extract_dir = Path(sys.argv[1])
member_list_path = Path(sys.argv[2])
root_dir = Path(sys.argv[3])
trusted_reference_path = Path(sys.argv[4]) if sys.argv[4] else None
manifest_relpath = "BUNDLE_MANIFEST.txt"

LEGACY_BUNDLE_EXCLUDES = [
    ".git",
    ".worktrees",
    ".tasklists",
    ".runlogs",
    ".codex",
    ".claude",
    ".serena",
    ".gemini",
    "artifacts",
    "logs",
    "node_modules",
    "dist",
    "coverage",
    "*.tar.gz",
    "BUNDLE_MANIFEST.txt",
    ".idea",
    ".vscode",
    ".omc",
]

V1_BUNDLE_EXCLUDES = [
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

def fail(message: str) -> None:
    print(f"FAIL: {message}", file=sys.stderr)
    raise SystemExit(1)

def git_output(*args: str) -> str:
    return subprocess.check_output(
        ["git", "-C", str(root_dir), *args],
        text=True,
        stderr=subprocess.DEVNULL,
    ).strip()

def git_paths(*args: str) -> list[str]:
    output = subprocess.check_output(
        ["git", "-C", str(root_dir), *args],
        stderr=subprocess.DEVNULL,
    )
    if not output:
        return []
    return [path.decode("utf-8") for path in output.rstrip(b"\0").split(b"\0")]

def is_legacy_excluded_path(path: str) -> bool:
    exact_prefixes = (
        ".git",
        ".worktrees",
        ".tasklists",
        ".runlogs",
        ".codex",
        ".claude",
        ".serena",
        ".gemini",
        "artifacts",
        "logs",
        "node_modules",
        "dist",
        "coverage",
    )
    local_metadata = (".idea", ".vscode", ".omc")
    if any(path == item or path.startswith(f"{item}/") for item in exact_prefixes):
        return True
    if path == manifest_relpath:
        return True
    if fnmatch.fnmatchcase(path, "*.tar.gz"):
        return True
    return any(
        path == item
        or path.startswith(f"{item}/")
        or path.endswith(f"/{item}")
        or f"/{item}/" in path
        for item in local_metadata
    )

def is_v1_excluded_path(path: str) -> bool:
    parts = path.split("/")
    root_prefixes = (
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
    )
    nested_dirs = ("node_modules", "dist", "coverage", ".idea", ".vscode", ".omc")
    if path == manifest_relpath:
        return True
    if parts[0] in root_prefixes:
        return True
    if any(part in nested_dirs for part in parts):
        return True
    if any(part == ".env" or part.startswith(".env.") for part in parts):
        return True
    if path.endswith(".key") or ".key." in path:
        return True
    if path.endswith(".pem") or ".pem." in path:
        return True
    return fnmatch.fnmatchcase(path, "*.tar.gz")

def read_members() -> list[str]:
    members = member_list_path.read_text(encoding="utf-8").splitlines()
    if manifest_relpath not in members:
        fail("bundle manifest missing")
    return members

def parse_manifest_lines(lines: list[str]) -> tuple[dict[str, str], list[str]]:
    fields: dict[str, str] = {}
    file_lines: list[str] = []
    in_files = False
    for line in lines:
        if in_files:
            file_lines.append(line)
            continue
        if line == "files:":
            in_files = True
            continue
        if ": " in line:
            key, value = line.split(": ", 1)
            fields[key] = value
    return fields, file_lines if in_files else []

def read_manifest() -> tuple[dict[str, str], list[str]]:
    manifest_path = extract_dir / manifest_relpath
    if not manifest_path.is_file() or manifest_path.is_symlink():
        fail("bundle manifest missing")
    return parse_manifest_lines(manifest_path.read_text(encoding="utf-8").splitlines())

def sha256_file(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as handle:
        for chunk in iter(lambda: handle.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()

def payload_path(path: str) -> Path:
    candidate = extract_dir / path
    if not candidate.is_file() or candidate.is_symlink():
        fail(f"bundle member is not a regular file after extraction: {path}")
    return candidate

def parse_manifest_hash_entries(
    file_lines: list[str],
    expected_count: int | None,
    source_label: str,
) -> dict[str, str]:
    manifest_hashes: dict[str, str] = {}
    for line in file_lines:
        if not line:
            continue
        digest, separator, path = line.partition("  ")
        if not separator or len(digest) != 64 or any(char not in "0123456789abcdef" for char in digest):
            fail(f"invalid {source_label} file entry: {line}")
        if path == manifest_relpath:
            fail(f"{source_label} must not list itself")
        if path in manifest_hashes:
            fail(f"duplicate {source_label} file entry: {path}")
        manifest_hashes[path] = digest

    if expected_count is not None and expected_count != len(manifest_hashes):
        fail(f"{source_label} file_count mismatch: expected {expected_count}, got {len(manifest_hashes)}")
    return manifest_hashes

def payload_hashes(paths: list[str]) -> dict[str, str]:
    return {path: sha256_file(payload_path(path)) for path in paths}

def current_checkout_hashes(fields: dict[str, str]) -> dict[str, str]:
    policy = fields["policy"]
    tracked_only = fields["tracked_only"]
    if policy != "tracked-only" or tracked_only != "true":
        fail("trusted manifest required for non-tracked-only bundle verification")

    hashes: dict[str, str] = {}
    for path in git_paths("ls-files", "-z", "--cached"):
        if is_v1_excluded_path(path):
            continue
        source_path = root_dir / path
        if source_path.is_symlink() or not source_path.exists() or not source_path.is_file():
            fail(f"current checkout tracked file is unsafe or missing: {path}")
        hashes[path] = sha256_file(source_path)
    return hashes

def read_trusted_reference_hashes() -> dict[str, str]:
    if trusted_reference_path is None:
        fail("trusted reference path missing")
    if not trusted_reference_path.is_file() or trusted_reference_path.is_symlink():
        fail(f"trusted reference not found: {trusted_reference_path}")

    lines = trusted_reference_path.read_text(encoding="utf-8").splitlines()
    fields, file_lines = parse_manifest_lines(lines)
    if fields.get("format") == "hololive-review-bundle-v1":
        try:
            reference_count = int(fields["file_count"])
        except (KeyError, ValueError) as exc:
            raise SystemExit("FAIL: invalid trusted manifest file_count") from exc
        return parse_manifest_hash_entries(file_lines, reference_count, "trusted manifest")

    hashes: dict[str, str] = {}
    for line in lines:
        line = line.strip()
        if not line or line.startswith("#"):
            continue
        parts = line.split(None, 1)
        if len(parts) != 2:
            fail(f"invalid trusted checksum entry: {line}")
        digest, path = parts
        if len(digest) != 64 or any(char not in "0123456789abcdef" for char in digest):
            fail(f"invalid trusted checksum entry: {line}")
        if path == manifest_relpath:
            fail("trusted checksum must not list bundle manifest")
        if path in hashes:
            fail(f"duplicate trusted checksum entry: {path}")
        hashes[path] = digest
    if not hashes:
        fail("trusted reference has no file checksums")
    return hashes

def compare_hash_sets(actual: dict[str, str], expected: dict[str, str], expected_label: str) -> None:
    actual_paths = sorted(actual)
    expected_paths = sorted(expected)
    if actual_paths != expected_paths:
        if expected_label == "current checkout":
            fail("bundle contents differ from current checkout export policy")
        fail(f"bundle contents differ from {expected_label}")

    for path in expected_paths:
        if actual[path] != expected[path]:
            if expected_label == "current checkout":
                fail(f"bundle file contents differ from current checkout: {path}")
            fail(f"bundle file contents differ from {expected_label}: {path}")

def validate_v1_manifest(members: list[str], fields: dict[str, str], file_lines: list[str]) -> str:
    required = {
        "format",
        "mode",
        "policy",
        "tracked_only",
        "allowlist_file",
        "file_count",
        "excluded_patterns",
    }
    missing = sorted(required - fields.keys())
    if missing:
        fail(f"bundle manifest schema drift: {', '.join(missing)} missing")
    if fields["format"] != "hololive-review-bundle-v1":
        fail(f"unsupported bundle manifest format: {fields['format']}")
    if fields["mode"] != "source":
        fail(f"unsupported bundle mode: {fields['mode']}")
    if fields["policy"] not in {"tracked-only", "tracked-plus-allowlist"}:
        fail(f"unsupported bundle policy: {fields['policy']}")
    if fields["policy"] == "tracked-only" and fields["tracked_only"] != "true":
        fail(f"invalid tracked_only value: {fields['tracked_only']}")
    if fields["policy"] == "tracked-plus-allowlist" and fields["tracked_only"] != "false":
        fail(f"invalid tracked_only value: {fields['tracked_only']}")
    if fields["excluded_patterns"] != ",".join(V1_BUNDLE_EXCLUDES):
        fail("bundle manifest excluded_patterns drift")

    payload_members = sorted(path for path in members if path != manifest_relpath)
    for path in payload_members:
        if is_v1_excluded_path(path):
            fail(f"excluded path found in bundle: {path}")

    try:
        manifest_count = int(fields["file_count"])
    except ValueError as exc:
        raise SystemExit(f"FAIL: invalid bundle manifest file_count: {fields['file_count']}") from exc
    if manifest_count != len(payload_members):
        fail(f"bundle manifest file_count mismatch: expected {manifest_count}, got {len(payload_members)}")

    manifest_hashes = parse_manifest_hash_entries(file_lines, manifest_count, "bundle manifest")
    if sorted(manifest_hashes) != payload_members:
        fail("bundle contents differ from manifest file set")

    actual_hashes = payload_hashes(payload_members)
    compare_hash_sets(actual_hashes, manifest_hashes, "manifest")

    # 번들 내부 manifest는 무결성만 확인하며 신뢰 출처(authenticity)는 별도 기준으로 고정한다.
    if trusted_reference_path is None:
        expected_hashes = current_checkout_hashes(fields)
        expected_label = "current checkout"
    else:
        expected_hashes = read_trusted_reference_hashes()
        expected_label = "trusted reference"
    compare_hash_sets(actual_hashes, expected_hashes, expected_label)
    return expected_label

def legacy_content_sha256(base_dir: Path, paths: list[str]) -> str:
    outer = hashlib.sha256()
    for path in paths:
        digest = sha256_file(base_dir / path)
        outer.update(f"{digest}  {path}\n".encode("utf-8"))
    return outer.hexdigest()

def validate_legacy_manifest(members: list[str], fields: dict[str, str]) -> None:
    required = {
        "mode",
        "tracked_only",
        "branch",
        "commit",
        "included_files",
        "excluded_patterns",
        "content_sha256",
    }
    missing = sorted(required - fields.keys())
    if missing:
        fail(f"bundle manifest schema drift: {', '.join(missing)} missing")
    if fields["mode"] != "full":
        fail(f"unsupported bundle mode: {fields['mode']}")

    expected_excluded_patterns = ",".join(LEGACY_BUNDLE_EXCLUDES)
    if fields["excluded_patterns"] != expected_excluded_patterns:
        fail("bundle manifest excluded_patterns drift")
    if fields["commit"] != git_output("rev-parse", "HEAD"):
        fail("bundle manifest commit does not match current checkout")
    if fields["branch"] != git_output("rev-parse", "--abbrev-ref", "HEAD"):
        fail("bundle manifest branch does not match current checkout")

    expected_files = [
        path
        for path in git_paths("ls-files", "-z", "--cached")
        if (root_dir / path).exists() and not is_legacy_excluded_path(path)
    ]
    if fields["tracked_only"] == "false":
        expected_files.extend(
            path
            for path in git_paths("ls-files", "-z", "--others", "--exclude-standard")
            if (root_dir / path).exists() and not is_legacy_excluded_path(path)
        )
    elif fields["tracked_only"] != "true":
        fail(f"invalid tracked_only value: {fields['tracked_only']}")

    expected_files = sorted(set(expected_files))
    actual_files = sorted(path for path in members if path != manifest_relpath)

    for path in actual_files:
        if is_legacy_excluded_path(path):
            fail(f"excluded path found in bundle: {path}")

    if fields["included_files"] != str(len(actual_files)):
        fail(f"bundle manifest included_files mismatch: expected {fields['included_files']}, got {len(actual_files)}")
    if expected_files != actual_files:
        fail("bundle contents differ from current checkout export policy")

    expected_content_sha256 = legacy_content_sha256(root_dir, expected_files)
    actual_content_sha256 = legacy_content_sha256(extract_dir, actual_files)
    if fields["content_sha256"] != expected_content_sha256:
        fail("bundle manifest content_sha256 does not match current checkout")
    if fields["content_sha256"] != actual_content_sha256:
        fail("bundle file contents differ from manifest content hash")

members = read_members()
fields, file_lines = read_manifest()
if fields.get("format") == "hololive-review-bundle-v1":
    reference_label = validate_v1_manifest(members, fields, file_lines)
else:
    validate_legacy_manifest(members, fields)
    reference_label = "in-repo export policy"

print(f"OK: full bundle matches manifest and {reference_label}")
PY
