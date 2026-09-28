#!/usr/bin/env bash
# umask 077 checkout(파일 600·디렉터리 700)에서 빌드해도 이미지에 들어가는 Node 런타임 트리를
# runtime uid가 읽을 수 있는지 실제 컨테이너 안에서 확인한다. v7.0.0 배포에서 600 파일이 든 PO issuer
# 이미지가 uid 65532로 worker.mjs를 열지 못해(EACCES) 중앙 쌍이 자동 rollback됐다.
# 각 Node build stage를 검사한다. 최종 stage는 이 트리를 모드 변경 없이 복사하고 collector·alarm-worker만
# 소유자를 runtime uid로 바꾸므로, 소유자가 root인 build stage에서 읽히면 최종 이미지에서도 읽힌다.
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"

fail() {
  echo "[FAIL] $*" >&2
  exit 1
}

pass() {
  echo "[PASS] $*"
}

# 빌드는 kapu에서만 하므로 docker가 없으면 통과로 넘기지 않고 실패한다.
docker version >/dev/null 2>&1 || fail "docker daemon unavailable; run this image build check on kapu"

TMP_DIR="$(mktemp -d)"
tags=()
cleanup() {
  if ((${#tags[@]} > 0)); then
    docker image rm -f "${tags[@]}" >/dev/null 2>&1 || true
  fi
  rm -rf "${TMP_DIR}"
}
trap cleanup EXIT

collector=hololive/hololive-youtube-collector
alarm=hololive/hololive-alarm-worker
checkout="${TMP_DIR}/checkout"
mkdir -p "${checkout}"
# 작업 트리의 추적 파일을 umask 077로 풀어 제한적 umask에서 만든 checkout을 재현한다.
git -C "${ROOT_DIR}" ls-files -z -- \
  "${collector}/Dockerfile.po-sandbox" "${collector}/Dockerfile.po-sandbox.dockerignore" \
  "${collector}/Dockerfile" "${collector}/Dockerfile.dockerignore" \
  "${alarm}/Dockerfile" "${alarm}/Dockerfile.dockerignore" \
  "${collector}/po-sandbox/package.json" "${collector}/po-sandbox/package-lock.json" "${collector}/po-sandbox/src" \
  "${collector}/youtubejs/package.json" "${collector}/youtubejs/package-lock.json" "${collector}/youtubejs/src" \
  "${alarm}/xspaces/package.json" "${alarm}/xspaces/package-lock.json" "${alarm}/xspaces/src" |
  tar -C "${ROOT_DIR}" --null -T - -cf - |
  (umask 077 && tar -xf - --no-same-permissions -C "${checkout}")
fixture_mode="$(stat -c '%a' "${checkout}/${collector}/po-sandbox/src/worker.mjs")"
[[ "${fixture_mode}" == 600 ]] || fail "fixture must reproduce a umask 077 checkout (worker.mjs mode ${fixture_mode})"

# 심볼릭 링크는 따라가지 않고, 디렉터리는 읽기·탐색, 파일은 읽기 권한을 runtime uid로 확인한다.
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
  tag="hololive-umask-check-$$-${target}"
  tags+=("${tag}")
  docker buildx build --quiet --load --provenance=false --target "${target}" --tag "${tag}" \
    --file "${checkout}/${dockerfile}" "${checkout}" >/dev/null ||
    fail "${dockerfile} --target ${target} build failed"
  if ! docker run --rm --network none --user "${user}" --entrypoint /usr/local/bin/node "${tag}" \
    -e "${walker}" "${tree}"; then
    fail "${dockerfile} ${target}: ${tree} is not readable by runtime user ${user} after a umask 077 checkout"
  fi
  pass "${dockerfile} ${target}: ${tree} readable by ${user} after a umask 077 checkout"
}

check_stage "${collector}/Dockerfile.po-sandbox" package-build /app/po-sandbox 65532:1000
check_stage "${collector}/Dockerfile" youtubejs-build /app/youtubejs 1000:1000
check_stage "${alarm}/Dockerfile" xspaces-build /app/xspaces 1000:1000
