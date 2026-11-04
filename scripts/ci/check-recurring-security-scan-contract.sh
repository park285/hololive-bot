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

# 배포 원본은 Quadlet unit의 기본 image다(중앙 + standby 참고 unit).
sed -n 's/^Environment=[A-Z0-9_]*_IMAGE=//p' deploy/quadlet/*/*.container | LC_ALL=C sort -u >"$tmp_dir/final-images"
cut -d'|' -f3 scripts/ci/final-image-scan-manifest.txt | LC_ALL=C sort >"$tmp_dir/manifest-images"
cmp -s "$tmp_dir/manifest-images" "$tmp_dir/final-images" ||
  fail "final image manifest does not cover the exact current production Quadlet image corpus"

if awk -F'|' '
  NF != 3 || $1 == "" || $2 != "linux/arm64" || $3 == "" { exit 1 }
  $3 ~ /@sha256:/ && $1 != "remote" { exit 1 }
  $3 !~ /@sha256:/ && $1 != "local" { exit 1 }
' scripts/ci/final-image-scan-manifest.txt; then
  :
else
  fail "final image manifest must scan arm64 local builds locally and exact arm64 external images remotely"
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
