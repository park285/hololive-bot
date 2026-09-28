#!/usr/bin/env bash
set -euo pipefail

root_dir="$(git rev-parse --show-toplevel)"
cd "$root_dir"

fail() {
  echo "recurring security scan contract failed: $*" >&2
  exit 1
}

tmp_dir="$(mktemp -d)"
trap 'rm -rf "$tmp_dir"' EXIT

git ls-files '*package-lock.json' | LC_ALL=C sort -u >"$tmp_dir/npm-locks"
cmp -s scripts/ci/npm-audit-manifest.txt "$tmp_dir/npm-locks" ||
  fail "npm audit manifest does not cover the exact tracked lockfile corpus"

{
  LIVE_LOGS_PATH=/tmp LIVE_DB_BACKUP_PATH=/tmp \
    docker compose --env-file deploy/compose/build-only.env.sample \
      -f deploy/compose/docker-compose.prod.yml \
      -f deploy/compose/docker-compose.live-compat.yml config --images
  docker compose --env-file deploy/compose/build-only.env.sample \
    -f deploy/compose/docker-compose.standby.yml config --images
} | LC_ALL=C sort -u >"$tmp_dir/final-images"
cut -d'|' -f3 scripts/ci/final-image-scan-manifest.txt >"$tmp_dir/manifest-images"
cmp -s "$tmp_dir/manifest-images" "$tmp_dir/final-images" ||
  fail "final image manifest does not cover the exact current production Compose corpus"

if awk -F'|' '
  NF != 3 || $1 == "" || $2 != "linux/arm64" || $3 == "" { exit 1 }
  $3 ~ /@sha256:/ && $1 != "remote" { exit 1 }
  $3 !~ /@sha256:/ && $1 != "local" { exit 1 }
' scripts/ci/final-image-scan-manifest.txt; then
  :
else
  fail "final image manifest must scan arm64 local builds locally and exact arm64 external images remotely"
fi

LIVE_LOGS_PATH=/tmp LIVE_DB_BACKUP_PATH=/tmp \
  docker compose --env-file deploy/compose/build-only.env.sample \
    -f deploy/compose/docker-compose.prod.yml build --print >"$tmp_dir/production-bake.json"
grep -Fq '"type=provenance,mode=max"' "$tmp_dir/production-bake.json" ||
  fail "production builds must retain maximum provenance attestations"
grep -Fq '"type=sbom"' "$tmp_dir/production-bake.json" ||
  fail "production builds must retain SBOM attestations"

LIVE_LOGS_PATH=/tmp LIVE_DB_BACKUP_PATH=/tmp \
  docker compose --env-file deploy/compose/build-only.env.sample \
    -f deploy/compose/docker-compose.prod.yml \
    -f deploy/compose/docker-compose.security-scan.yml build --print >"$tmp_dir/security-scan-bake.json"
# Compose 5는 비활성화도 disabled=true로 출력하므로 필드 존재만으로 판정하지 않는다.
if ! jq -e -f scripts/ci/disabled-bake-attestations.jq "$tmp_dir/security-scan-bake.json" >/dev/null; then
  fail "disposable local-image scan builds must not request unsupported attestations"
fi

# 최종 이미지 스캔은 발견 시 실패하고 어떤 억제도 두지 않는다. 정책 배열과, 실제 스캐너가
# 가짜 trivy에 넘긴 인자를 같은 규칙으로 검사한다.
. scripts/ci/final-image-scan-policy.sh
check_trivy_args() {
  local label="$1" arg key value
  shift
  local -A count=() seen=()
  local -a positional=()
  while (($#)); do
    arg="$1"
    shift
    [[ "$arg" != *trivyignore* ]] || fail "$label: trivyignore input is not allowed: $arg"
    if [[ "$arg" != --* ]]; then
      positional+=("$arg")
      continue
    fi
    key="${arg%%=*}"
    if [[ "$arg" == *=* ]]; then
      value="${arg#*=}"
    else
      case "$key" in
        --config | --ignorefile | --exit-code | --scanners | --severity | --format | --output | --image-src | --platform)
          (($#)) || fail "$label: $key has no value"
          value="$1"
          shift
          ;;
        *) value=true ;;
      esac
    fi
    case "$key" in
      --config | --ignorefile | --ignore-unfixed | --exit-code | --no-progress | --scanners | --severity | --format | --output | --image-src | --platform) ;;
      *) fail "$label: flag outside the no-suppression allowlist: $arg" ;;
    esac
    count[$key]=$((${count[$key]:-0} + 1))
    seen[$key]="$value"
  done
  for key in "${!count[@]}"; do
    [[ "${count[$key]}" == 1 ]] || fail "$label: $key must appear exactly once"
  done
  [[ "${seen[--config]-}" == /dev/null ]] || fail "$label: --config must be /dev/null"
  [[ "${seen[--ignorefile]-}" == /dev/null ]] || fail "$label: --ignorefile must be /dev/null"
  [[ "${seen[--ignore-unfixed]-}" == false ]] || fail "$label: --ignore-unfixed=false is required"
  [[ "${seen[--exit-code]-}" == 1 ]] || fail "$label: --exit-code 1 is required"
  [[ "${seen[--scanners]-}" == vuln ]] || fail "$label: --scanners vuln is required"
  [[ "${seen[--severity]-}" == UNKNOWN,LOW,MEDIUM,HIGH,CRITICAL ]] ||
    fail "$label: every severity must be reported"
  [[ "${positional[0]-}" == image ]] || fail "$label: trivy subcommand must be image"
  scanned_positional=("${positional[@]:1}")
  scanned_image_src="${seen[--image-src]-}"
  scanned_platform="${seen[--platform]-}"
}
check_trivy_args "final-image-scan-policy.sh" "${FINAL_IMAGE_TRIVY_ARGS[@]}"
((${#scanned_positional[@]} == 0)) || fail "policy args must not name an image"

stub_bin="$tmp_dir/stub-bin"
mkdir -p "$stub_bin" "$tmp_dir/trivy-calls" "$tmp_dir/scan-tmp"
cat >"$stub_bin/trivy" <<'SH'
#!/usr/bin/env bash
set -eu
if [[ "$*" == --version ]]; then echo "Version: $STUB_TRIVY_VERSION"; exit 0; fi
call="$STUB_TRIVY_CALLS/$(find "$STUB_TRIVY_CALLS" -name '*.args' | wc -l).args"
printf '%s\0' "$@" >"$call"
env | grep '^TRIVY_' >"${call%.args}.env" || true
while (($#)) && [[ "$1" != --output ]]; do shift; done
echo '{"SchemaVersion":2,"Results":[{"Target":"stub","Type":"alpine"}]}' >"$2"
SH
cat >"$stub_bin/docker" <<'SH'
#!/usr/bin/env bash
set -eu
[[ "$1 $2" == "image inspect" ]] || exit 2
if [[ "$4" == *Architecture* ]]; then echo linux/arm64; else echo "sha256:$(printf '0%.0s' {1..64})"; fi
SH
printf '%s\n' '#!/usr/bin/env bash' 'echo govulncheck@v1.8.0' >"$stub_bin/govulncheck"
chmod +x "$stub_bin"/*
remote_image="$(grep -m1 '^remote|' scripts/ci/final-image-scan-manifest.txt)"
local_image="$(grep -m1 '^local|' scripts/ci/final-image-scan-manifest.txt)"
printf '%s\n' "$remote_image" "$local_image" >"$tmp_dir/stub-manifest"
# env -i: 호출 환경의 TRIVY_* 가 아니라 스캐너 자신이 넘기는 설정만 관찰한다.
env -i PATH="$stub_bin:$PATH" HOME="$tmp_dir" TMPDIR="$tmp_dir/scan-tmp" \
  STUB_TRIVY_VERSION="$FINAL_IMAGE_TRIVY_VERSION" STUB_TRIVY_CALLS="$tmp_dir/trivy-calls" \
  bash scripts/ci/run-final-image-scan.sh "$tmp_dir/stub-manifest" >"$tmp_dir/scan.log" 2>&1 ||
  fail "final-image scanner failed against a clean stub report: $(cat "$tmp_dir/scan.log")"
expected=("remote|remote|${remote_image##*|}" "docker|local|sha256:$(printf '0%.0s' {1..64})")
[[ "$(find "$tmp_dir/trivy-calls" -name '*.args' | wc -l)" == 2 ]] ||
  fail "final-image scanner must invoke trivy once per manifest image"
for index in 0 1; do
  [[ ! -s "$tmp_dir/trivy-calls/$index.env" ]] ||
    fail "final-image scanner must not configure trivy through TRIVY_* environment"
  mapfile -d '' -t scanned_args <"$tmp_dir/trivy-calls/$index.args"
  check_trivy_args "run-final-image-scan.sh call $index" "${scanned_args[@]}"
  IFS='|' read -r want_src want_source want_image <<<"${expected[index]}"
  [[ "$scanned_image_src" == "$want_src" && "$scanned_platform" == linux/arm64 ]] ||
    fail "$want_source images must be scanned from $want_src for linux/arm64"
  [[ "${scanned_positional[*]}" == "$want_image" ]] ||
    fail "$want_source scan must target exactly $want_image, got: ${scanned_positional[*]}"
done

workflow=.github/workflows/security.yml
if grep -Eq '^[[:space:]-]*uses:[[:space:]]*aquasecurity/' "$workflow"; then
  fail "security workflow must install the hash-pinned Trivy release, not an aquasecurity action"
fi
trivy_step="$(awk '/^      - name: Install exact Trivy$/ { f = 1; print; next } f && /^      - name:/ { f = 0 } f' "$workflow")"
grep -Fxq "          TRIVY_VERSION: \"$FINAL_IMAGE_TRIVY_VERSION\"" <<<"$trivy_step" ||
  fail "security workflow must install Trivy $FINAL_IMAGE_TRIVY_VERSION required by the scanner"
# shellcheck disable=SC2016 # workflow의 셸 식을 글자 그대로 찾는다.
if ! grep -Eq '^          TRIVY_LINUX_AMD64_SHA256: [0-9a-f]{64}$' <<<"$trivy_step" ||
  ! grep -Fq '"${TRIVY_LINUX_AMD64_SHA256}" "${archive}" | sha256sum --check -' <<<"$trivy_step"; then
  fail "security workflow must verify the Trivy archive against a pinned sha256"
fi

echo "recurring npm and final-image security scan contract passed"
