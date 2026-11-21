#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"

fail() {
  echo "[FAIL] $*" >&2
  exit 1
}

pass() {
  echo "[PASS] $*"
}

if [[ -n "${CONTAINER_HOST:-}${CONTAINER_CONNECTION:-}" ]] ||
  [[ "$(podman info --format '{{.Host.Security.Rootless}}' 2>/dev/null)" != true ]]; then
  fail "local rootless Podman unavailable; run this image build check on kapu"
fi

TMP_DIR="$(mktemp -d)"
tags=()
cleanup() {
  if ((${#tags[@]} > 0)); then
    podman image rm -f "${tags[@]}" >/dev/null 2>&1 || true
  fi
  rm -rf "${TMP_DIR}"
}
trap cleanup EXIT

collector=hololive/hololive-youtube-collector
alarm=hololive/hololive-alarm-worker
checkout="${TMP_DIR}/checkout"
mkdir -p "${checkout}"
git -C "${ROOT_DIR}" ls-files -z -- \
  "${collector}/Dockerfile.po-sandbox" "${collector}/Dockerfile.po-sandbox.dockerignore" \
  "${collector}/Dockerfile" "${collector}/Dockerfile.dockerignore" \
  "${alarm}/Dockerfile" "${alarm}/Dockerfile.dockerignore" \
  "${collector}/po-sandbox/package.json" "${collector}/po-sandbox/package-lock.json" "${collector}/po-sandbox/src" \
  "${collector}/youtubejs/package.json" "${collector}/youtubejs/package-lock.json" "${collector}/youtubejs/src" \
  "${collector}/eventgrade/server.mjs" \
  "${alarm}/xspaces/package.json" "${alarm}/xspaces/package-lock.json" "${alarm}/xspaces/src" |
  tar -C "${ROOT_DIR}" --null -T - -cf - |
  (umask 077 && tar -xf - --no-same-permissions -C "${checkout}")
fixture_mode="$(stat -c '%a' "${checkout}/${collector}/po-sandbox/src/worker.mjs")"
[[ "${fixture_mode}" == 600 ]] || fail "fixture must reproduce a umask 077 checkout (worker.mjs mode ${fixture_mode})"

# shellcheck disable=SC2016 # Node 코드이며 shell 확장이 아니다.
walker='
const fs = require("fs"), path = require("path");
const { R_OK, X_OK } = fs.constants;
const denied = [];
let files = 0;
(function walk(p) {
  const st = fs.lstatSync(p);
  if (st.isSymbolicLink()) return;
  if (st.isDirectory()) {
    try { fs.accessSync(p, R_OK | X_OK); } catch { denied.push(`${p}/`); return; }
    for (const name of fs.readdirSync(p)) walk(path.join(p, name));
    return;
  }
  files++;
  try { fs.accessSync(p, R_OK); } catch { denied.push(p); }
})(process.argv[1]);
if (files === 0) { console.error(`no files under ${process.argv[1]}`); process.exit(1); }
if (denied.length > 0) { console.error(denied.slice(0, 20).join("\n")); process.exit(1); }
'

check_stage() {
  local dockerfile="$1" target="$2" tree="$3" user="$4" tag
  tag="localhost/hololive-umask-check-$$-${target}"
  tags+=("${tag}")
  podman build --quiet --target "${target}" --tag "${tag}" \
    --file "${checkout}/${dockerfile}" "${checkout}" >/dev/null ||
    fail "${dockerfile} --target ${target} build failed"
  if ! podman run --rm --pull=never --network none --user "${user}" --entrypoint /usr/local/bin/node "${tag}" \
    -e "${walker}" "${tree}"; then
    fail "${dockerfile} ${target}: ${tree} is not readable by runtime user ${user} after a umask 077 checkout"
  fi
  pass "${dockerfile} ${target}: ${tree} readable by ${user} after a umask 077 checkout"
}

check_stage "${collector}/Dockerfile.po-sandbox" package-build /app/po-sandbox 65532:1000
check_stage "${collector}/Dockerfile" youtubejs-build /app/youtubejs 1000:1000
check_stage "${collector}/Dockerfile" eventgrade-build /app/eventgrade 1000:1000
check_stage "${alarm}/Dockerfile" xspaces-build /app/xspaces 1000:1000
