#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
BUILD="${ROOT_DIR}/scripts/build/build-youtube-collector-go.sh"
MAKEFILE="${ROOT_DIR}/hololive/hololive-youtube-collector/Makefile"
GATE="${ROOT_DIR}/scripts/ci/public-pr-go-gate.sh"

TMP_ROOT="$(mktemp -d)"
trap 'rm -rf "${TMP_ROOT}"' EXIT

PASSED=0

assert_exit() {
  local name="$1"
  local expected="$2"
  local actual="$3"
  if [[ "${actual}" -ne "${expected}" ]]; then
    printf 'not ok - %s: expected exit %s got %s\n' "${name}" "${expected}" "${actual}" >&2
    exit 1
  fi
  PASSED=$((PASSED + 1))
  printf 'ok - %s\n' "${name}"
}

assert_contains() {
  local name="$1"
  local haystack="$2"
  local needle="$3"
  if [[ "${haystack}" != *"${needle}"* ]]; then
    printf 'not ok - %s missing %q\n%s\n' "${name}" "${needle}" "${haystack}" >&2
    exit 1
  fi
  PASSED=$((PASSED + 1))
  printf 'ok - %s\n' "${name}"
}

check_artifact() {
  local dir="$1"
  local expected_version="${2:-hardening-test}"
  sh "${ROOT_DIR}/scripts/build/check-youtube-collector-go-artifact.sh" "${dir}" \
    --version "${expected_version}" \
    --revision "${revision}" \
    --goos linux \
    --goarch amd64 \
    --goamd64 v1
}

expect_artifact_failure() {
  local name="$1"
  local dir="$2"
  local needle="$3"
  local output
  local status
  set +e
  output="$(check_artifact "${dir}" 2>&1)"
  status=$?
  set -e
  if [[ "${status}" -eq 0 ]]; then
    printf 'not ok - %s: checker unexpectedly passed\n' "${name}" >&2
    exit 1
  fi
  assert_contains "${name}" "${output}" "${needle}"
}

revision="$(git -C "${ROOT_DIR}" rev-parse --verify 'HEAD^{commit}')"

set +e
bad_out="$(sh "${BUILD}" \
  --output-dir "${TMP_ROOT}/bad-rev" \
  --version hardening-test \
  --revision not-a-revision \
  --goos linux --goarch amd64 --goamd64 v1 2>&1)"
bad_status=$?
set -e
assert_exit "invalid revision is rejected" 2 "${bad_status}"
assert_contains "invalid revision names the contract" "${bad_out}" "40 lowercase hex"

set +e
unknown_out="$(sh "${BUILD}" \
  --output-dir "${TMP_ROOT}/unknown-rev" \
  --version hardening-test \
  --revision unknown \
  --goos linux --goarch amd64 --goamd64 v1 2>&1)"
unknown_status=$?
set -e
assert_exit "unknown revision without opt-in is rejected" 2 "${unknown_status}"
assert_contains "unknown revision names the contract" "${unknown_out}" "40 lowercase hex"

set +e
unsafe_version_out="$(sh "${BUILD}" \
  --output-dir "${TMP_ROOT}/unsafe-version" \
  --version 'bad version' \
  --revision "${revision}" \
  --goos linux --goarch amd64 --goamd64 v1 2>&1)"
unsafe_version_status=$?
set -e
assert_exit "unsafe version is rejected" 2 "${unsafe_version_status}"
assert_contains "unsafe version names the contract" "${unsafe_version_out}" "ASCII letters"

out_dir="${TMP_ROOT}/prod"
sh "${BUILD}" \
  --output-dir "${out_dir}" \
  --version hardening-test \
  --revision "${revision}" \
  --goos linux --goarch amd64 --goamd64 v1

python3 - "${out_dir}/manifest.json" "${revision}" <<'PY'
import json
import sys
from pathlib import Path

manifest = json.loads(Path(sys.argv[1]).read_text())
revision = sys.argv[2]
errors = []
if manifest.get("source_revision") != revision:
    errors.append("source_revision")
if manifest.get("version") != "hardening-test":
    errors.append("version")
go = manifest.get("go") or {}
if go.get("goos") != "linux" or go.get("goarch") != "amd64" or go.get("goamd64") != "v1":
    errors.append("go triple")
if errors:
    raise SystemExit("manifest identity disagree: " + ", ".join(errors))
PY
check_artifact "${out_dir}"
PASSED=$((PASSED + 1))
printf 'ok - manifest, checksums, build IDs, targets, and go version -m metadata agree\n'

python3 - "${out_dir}/manifest.json" "${revision}" <<'PY'
import json
import sys
from pathlib import Path

manifest = json.loads(Path(sys.argv[1]).read_text())
revision = sys.argv[2]
assert manifest["binaries"]["bin/youtube-collector"]["build_id"] == f"youtube-collector/hardening-test/{revision}"
assert manifest["binaries"]["bin/healthcheck"]["build_id"] == f"healthcheck/hardening-test/{revision}"
assert manifest["binaries"]["bin/healthcheck"]["package"].endswith("/cmd/runtime/healthcheck")
PY
PASSED=$((PASSED + 1))
printf 'ok - manifest records collector and healthcheck target identities\n'

identity_fixture="${TMP_ROOT}/bad-identity"
cp -a "${out_dir}" "${identity_fixture}"
python3 - "${identity_fixture}/manifest.json" <<'PY'
import json
import sys
from pathlib import Path

path = Path(sys.argv[1])
manifest = json.loads(path.read_text())
manifest["source_revision"] = "0" * 40
manifest["version"] = "wrong-version"
manifest["go"]["goos"] = "darwin"
manifest["go"]["goarch"] = "arm64"
manifest["go"]["goamd64"] = "v4"
path.write_text(json.dumps(manifest) + "\n")
PY
expect_artifact_failure "checker rejects manifest identity mutation" "${identity_fixture}" "source_revision"

hash_fixture="${TMP_ROOT}/bad-manifest-hash"
cp -a "${out_dir}" "${hash_fixture}"
python3 - "${hash_fixture}/manifest.json" <<'PY'
import json
import sys
from pathlib import Path

path = Path(sys.argv[1])
manifest = json.loads(path.read_text())
manifest["files"]["bin/youtube-collector"] = "0" * 64
path.write_text(json.dumps(manifest) + "\n")
PY
expect_artifact_failure "checker rejects manifest file hash mutation" "${hash_fixture}" "manifest checksum"

sums_fixture="${TMP_ROOT}/bad-sha256sums"
cp -a "${out_dir}" "${sums_fixture}"
printf '%064d  %s\n' 0 bin/youtube-collector > "${sums_fixture}/sha256sums.txt"
expect_artifact_failure "checker rejects sha256sums mutation" "${sums_fixture}" "sha256sums.txt"

health_fixture="${TMP_ROOT}/bad-healthcheck-target"
cp -a "${out_dir}" "${health_fixture}"
cp "${health_fixture}/bin/youtube-collector" "${health_fixture}/bin/healthcheck"
python3 - "${health_fixture}" <<'PY'
import hashlib
import json
import sys
from pathlib import Path

root = Path(sys.argv[1])
manifest_path = root / "manifest.json"
manifest = json.loads(manifest_path.read_text())
hashes = {}
for name in ("bin/youtube-collector", "bin/healthcheck"):
    hashes[name] = hashlib.sha256((root / name).read_bytes()).hexdigest()
manifest["files"] = hashes
manifest_path.write_text(json.dumps(manifest) + "\n")
(root / "sha256sums.txt").write_text(
    "".join(f"{hashes[name]}  {name}\n" for name in ("bin/youtube-collector", "bin/healthcheck"))
)
PY
expect_artifact_failure "checker rejects collector substituted for healthcheck" "${health_fixture}" "bin/healthcheck package"

set +e
wrong_expected_out="$(check_artifact "${out_dir}" another-version 2>&1)"
wrong_expected_status=$?
set -e
assert_exit "checker rejects wrong expected version" 1 "${wrong_expected_status}"
assert_contains "checker reports wrong expected version" "${wrong_expected_out}" "version"

set +e
shared_out="$(bash "${GATE}" hololive/hololive-shared test-prod 2>&1)"
shared_test_status=$?
shared_build_out="$(bash "${GATE}" hololive/hololive-shared build-prod 2>&1)"
shared_build_status=$?
set -e
assert_exit "test-prod on non-collector module exits 2" 2 "${shared_test_status}"
assert_contains "test-prod usage names collector module" "${shared_out}" "only valid for hololive/hololive-youtube-collector"
assert_exit "build-prod on non-collector module exits 2" 2 "${shared_build_status}"
assert_contains "build-prod usage names collector module" "${shared_build_out}" "only valid for hololive/hololive-youtube-collector"

make_n="$(make -C "${ROOT_DIR}/hololive/hololive-youtube-collector" -n test-prod build-prod 2>&1)"
assert_contains "Makefile test-prod delegates to go-gate" "${make_n}" \
  "public-pr-go-gate.sh hololive/hololive-youtube-collector test-prod"
assert_contains "Makefile build-prod delegates to go-gate" "${make_n}" \
  "public-pr-go-gate.sh hololive/hololive-youtube-collector build-prod"
if [[ "${make_n}" == *"go test -tags sonic"* ]]; then
  printf 'not ok - Makefile still implements a second sonic test path\n%s\n' "${make_n}" >&2
  exit 1
fi
PASSED=$((PASSED + 1))
printf 'ok - Makefile has no second sonic test implementation\n'
if grep -Eq '^build-bin:' "${MAKEFILE}"; then
  printf 'not ok - Makefile still exposes the retired build-bin alias\n' >&2
  exit 1
fi
PASSED=$((PASSED + 1))
printf 'ok - Makefile retired the build-bin compatibility alias\n'

printf 'ok - %s collector production build entrypoint checks passed\n' "${PASSED}"
