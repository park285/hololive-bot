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

# Compose 5.5.1의 build --print는 bake 정의만 출력하고 이미지를 만들지 않아, 빌드한 이미지가 없다는
# "No services to build" 경고를 항상 낸다(pkg/compose/build.go Build). 출력 전용 경로의 이 한 줄만 거르고
# 나머지 stderr(미설정 변수 경고 등)는 그대로 보인다.
print_bake() {
  local output="$1" status=0
  shift
  LIVE_LOGS_PATH=/tmp LIVE_DB_BACKUP_PATH=/tmp \
    docker compose --env-file deploy/compose/build-only.env.sample "$@" build --print \
    >"$output" 2>"$tmp_dir/bake-print.err" || status=$?
  grep -Fv 'msg="No services to build"' "$tmp_dir/bake-print.err" >&2 || true
  return "$status"
}

print_bake "$tmp_dir/production-bake.json" -f deploy/compose/docker-compose.prod.yml
# 모든 production target이 최대 provenance와 SBOM을 요청해야 한다. PO issuer는 build-po-sandbox-artifact.sh가
# rootfs tar로 export해 배포하므로 attestation을 실을 수 없어 같은 설정으로 끈다.
if ! jq -e '
  (.target | type == "object" and length > 0)
  and all(.target | to_entries[] | select(.key != "youtube-po-c");
    (.value.attest // []) as $attest
    | ($attest | index("type=provenance,mode=max")) != null
      and ($attest | index("type=sbom")) != null
  )
' "$tmp_dir/production-bake.json" >/dev/null; then
  fail "production builds must retain maximum provenance and SBOM attestations"
fi

print_bake "$tmp_dir/security-scan-bake.json" \
  -f deploy/compose/docker-compose.prod.yml -f deploy/compose/docker-compose.security-scan.yml
# Compose 5는 비활성화도 disabled=true로 출력하므로 필드 존재만으로 판정하지 않는다.
if ! jq -e -f scripts/ci/disabled-bake-attestations.jq "$tmp_dir/security-scan-bake.json" >/dev/null; then
  fail "disposable local-image scan builds must not request unsupported attestations"
fi

# 최종 이미지 스캔 정책은 발견 시 실패하고 어떤 억제도 두지 않는다.
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
}
check_trivy_args "final-image-scan-policy.sh" "${FINAL_IMAGE_TRIVY_ARGS[@]}"
((${#scanned_positional[@]} == 0)) || fail "policy args must not name an image"

echo "recurring npm and final-image security scan contract passed"
