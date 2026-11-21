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

sed -n 's/^Environment=[A-Z0-9_]*_IMAGE=//p' deploy/quadlet/*/*.container | LC_ALL=C sort -u >"$tmp_dir/final-images"
[[ -s "$tmp_dir/final-images" ]] || fail "no production Quadlet images found"
cut -d'|' -f3 scripts/ci/final-image-scan-manifest.txt | LC_ALL=C sort >"$tmp_dir/manifest-images"
cmp -s "$tmp_dir/manifest-images" "$tmp_dir/final-images" ||
  fail "final image manifest does not cover the exact current production Quadlet image corpus"

awk -F'|' '
  NF != 3 || $1 != "local" || $2 != "linux/arm64" || $3 == "" { exit 1 }
' scripts/ci/final-image-scan-manifest.txt || fail "final image manifest must list only arm64 local builds"

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
        --config | --ignorefile | --exit-code | --scanners | --severity)
          (($#)) || fail "$label: $key has no value"
          value="$1"
          shift
          ;;
        *) value=true ;;
      esac
    fi
    case "$key" in
      --config | --ignorefile | --ignore-unfixed | --exit-code | --no-progress | --scanners | --severity) ;;
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
  [[ "${#positional[@]}" == 1 && "${positional[0]-}" == image ]] || fail "$label: expected only the image subcommand"
}
check_trivy_args "final-image-scan-policy.sh" "${FINAL_IMAGE_TRIVY_ARGS[@]}"

echo "final image scan inventory and security policy passed"