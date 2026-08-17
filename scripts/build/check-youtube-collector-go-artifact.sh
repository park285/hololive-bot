#!/bin/sh
set -eu

usage() {
  echo "usage: $0 <output-dir> --version <version> --revision <40-lower-hex> --goos <goos> --goarch <goarch> --goamd64 <goamd64>" >&2
  exit 2
}

[ $# -eq 11 ] || usage
output_dir=$1
shift
expected_version=
expected_revision=
expected_goos=
expected_goarch=
expected_goamd64=
while [ $# -gt 0 ]; do
  case "$1" in
    --version) expected_version=$2 ;;
    --revision) expected_revision=$2 ;;
    --goos) expected_goos=$2 ;;
    --goarch) expected_goarch=$2 ;;
    --goamd64) expected_goamd64=$2 ;;
    *) usage ;;
  esac
  shift 2
done

case "${output_dir}" in
  /*) ;;
  *)
    echo "output-dir must be an absolute path" >&2
    exit 2
    ;;
esac

manifest="${output_dir}/manifest.json"
sums="${output_dir}/sha256sums.txt"
collector="${output_dir}/bin/youtube-collector"
healthcheck="${output_dir}/bin/healthcheck"
for required in "${manifest}" "${sums}" "${collector}" "${healthcheck}"; do
  [ -f "${required}" ] || {
    echo "artifact file missing: ${required}" >&2
    exit 1
  }
done
[ -x "${collector}" ] || { echo "collector binary is not executable: ${collector}" >&2; exit 1; }
[ -x "${healthcheck}" ] || { echo "healthcheck binary is not executable: ${healthcheck}" >&2; exit 1; }

collector_meta=$(go version -m "${collector}")
health_meta=$(go version -m "${healthcheck}")
collector_build_id=$(go tool buildid "${collector}")
health_build_id=$(go tool buildid "${healthcheck}")

python3 - \
  "${output_dir}" \
  "${expected_version}" "${expected_revision}" \
  "${expected_goos}" "${expected_goarch}" "${expected_goamd64}" \
  "${collector_meta}" "${health_meta}" \
  "${collector_build_id}" "${health_build_id}" <<'PY'
import hashlib
import json
import re
import sys
from pathlib import Path

output_dir = Path(sys.argv[1])
expected_version, expected_revision = sys.argv[2:4]
expected_goos, expected_goarch, expected_goamd64 = sys.argv[4:7]
collector_meta, health_meta = sys.argv[7:9]
collector_build_id, health_build_id = sys.argv[9:11]

manifest = json.loads((output_dir / "manifest.json").read_text(encoding="utf-8"))


def build_metadata(meta: str) -> tuple[str, dict[str, str]]:
    package = ""
    settings: dict[str, str] = {}
    for raw in meta.splitlines():
        line = raw.strip()
        if line.startswith("path\t"):
            package = line.split("\t", 1)[1]
            continue
        if not line.startswith("build"):
            continue
        payload = line[5:].lstrip()
        if "=" not in payload:
            continue
        key, value = payload.split("=", 1)
        settings[key] = value
    return package, settings


def file_sha256(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as handle:
        for chunk in iter(lambda: handle.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


errors: list[str] = []
go = manifest.get("go")
binaries = manifest.get("binaries")
files = manifest.get("files")
if manifest.get("schema_version") != 1:
    errors.append(f"schema_version={manifest.get('schema_version')}")
if manifest.get("source_revision") != expected_revision:
    errors.append("source_revision")
if manifest.get("version") != expected_version:
    errors.append("version")
if not re.fullmatch(r"[0-9a-f]{40}", expected_revision):
    errors.append("expected revision format")
if not isinstance(go, dict):
    errors.append("go metadata")
    go = {}
if go.get("cgo_enabled") is not False:
    errors.append("manifest cgo_enabled")
if go.get("goos") != expected_goos:
    errors.append("manifest GOOS")
if go.get("goarch") != expected_goarch:
    errors.append("manifest GOARCH")
if go.get("goamd64") != expected_goamd64:
    errors.append("manifest GOAMD64")
if go.get("pgo") != "off":
    errors.append("manifest pgo")
if go.get("tags") != ["sonic"]:
    errors.append("manifest tags")

expected_binaries = {
    "bin/youtube-collector": {
        "package": "github.com/kapu/hololive-youtube-collector/cmd/runtime/youtube-collector",
        "build_id": f"youtube-collector/{expected_version}/{expected_revision}",
        "tags": ["sonic"],
    },
    "bin/healthcheck": {
        "package": "github.com/kapu/hololive-youtube-collector/cmd/runtime/healthcheck",
        "build_id": f"healthcheck/{expected_version}/{expected_revision}",
        "tags": [],
    },
}
if binaries != expected_binaries:
    errors.append("binary manifest metadata")

metadata = {
    "bin/youtube-collector": (*build_metadata(collector_meta), collector_build_id),
    "bin/healthcheck": (*build_metadata(health_meta), health_build_id),
}
for name, (package, settings, build_id) in metadata.items():
    expected = expected_binaries[name]
    if package != expected["package"]:
        errors.append(f"{name} package")
    if build_id != expected["build_id"]:
        errors.append(f"{name} build ID")
    required_settings = {
        "-buildmode": "exe",
        "-compiler": "gc",
        "-trimpath": "true",
        "CGO_ENABLED": "0",
        "GOOS": expected_goos,
        "GOARCH": expected_goarch,
    }
    if expected_goarch == "amd64":
        required_settings["GOAMD64"] = expected_goamd64
    for key, value in required_settings.items():
        if settings.get(key) != value:
            errors.append(f"{name} {key}")
    if "-pgo" in settings:
        errors.append(f"{name} pgo")
    tags = [part for part in settings.get("-tags", "").split(",") if part]
    if tags != expected["tags"]:
        errors.append(f"{name} tags")

artifact_names = tuple(expected_binaries)
if not isinstance(files, dict) or set(files) != set(artifact_names):
    errors.append("manifest file set")
    files = {}
actual_hashes = {name: file_sha256(output_dir / name) for name in artifact_names}
for name, actual in actual_hashes.items():
    declared = files.get(name)
    if not isinstance(declared, str) or not re.fullmatch(r"[0-9a-f]{64}", declared):
        errors.append(f"{name} manifest checksum format")
    elif declared != actual:
        errors.append(f"{name} manifest checksum")

sums_lines = (output_dir / "sha256sums.txt").read_text(encoding="utf-8").splitlines()
expected_lines = [f"{actual_hashes[name]}  {name}" for name in artifact_names]
if sums_lines != expected_lines:
    errors.append("sha256sums.txt")

if errors:
    raise SystemExit("collector artifact disagree: " + ", ".join(errors))
PY
