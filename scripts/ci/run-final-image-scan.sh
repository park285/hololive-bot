#!/usr/bin/env bash
set -euo pipefail

root_dir="$(git rev-parse --show-toplevel)"
# 로컬 후보는 별도 manifest로 검사하여 공유 production 태그를 바꾸지 않는다.
[[ "$#" -le 1 ]] || { echo "usage: $0 [manifest]" >&2; exit 2; }
manifest="${1:-$root_dir/scripts/ci/final-image-scan-manifest.txt}"
. "$root_dir/scripts/ci/go-tooling.sh"
GOVULNCHECK_VERSION=v1.8.0
govulncheck_bin="$(ensure_govulncheck)"
report_dir="$(mktemp -d "${TMPDIR:-/tmp}/hololive-image-scan.XXXXXX")"
container_id=
trap 'if [[ -n "$container_id" ]]; then docker rm "$container_id" >/dev/null; fi' EXIT
echo "final image scan evidence: $report_dir"
image_index=0

if [[ "$(trivy --version | sed -n 's/^Version: //p')" != "0.74.0" ]]; then
  echo "final image scan requires Trivy 0.74.0" >&2
  exit 1
fi

while IFS='|' read -r source platform image; do
  [[ -n "$source$platform$image" ]] || continue
  [[ -n "$source" && -n "$platform" && -n "$image" ]] || {
    echo "incomplete final image scan manifest entry" >&2
    exit 1
  }
  scan_args=(image --config /dev/null --ignorefile /dev/null --ignore-unfixed=false --exit-code 1 --no-progress --scanners vuln --severity "UNKNOWN,LOW,MEDIUM,HIGH,CRITICAL")
  [[ "$platform" == linux/arm64 ]] || {
    echo "unsupported final image scan platform: $platform" >&2
    exit 1
  }
  case "$source" in
    local)
      actual_platform="$(docker image inspect --format '{{.Os}}/{{.Architecture}}' "$image")"
      [[ "$actual_platform" == "$platform" ]] || {
        echo "final image platform mismatch: $image is $actual_platform, want $platform" >&2
        exit 1
      }
      scan_args+=(--image-src docker --platform "$platform")
      # 태그가 바뀌어도 Trivy와 바이너리 추출이 같은 불변 이미지를 사용한다.
      image="$(docker image inspect --format '{{.Id}}' "$image")"
      ;;
    remote)
      [[ "$image" =~ @sha256:[0-9a-f]{64}$ ]] || {
        echo "remote final image scan requires an immutable digest" >&2
        exit 1
      }
      scan_args+=(--image-src remote --platform "$platform")
      ;;
    *)
      echo "unsupported final image scan source: $source" >&2
      exit 1
      ;;
  esac
  echo "trivy image: $source $platform $image"
  image_index=$((image_index + 1))
  report="$report_dir/$image_index.trivy.json"
  status=0
  trivy "${scan_args[@]}" --format json --output "$report" "$image" || status=$?
  [[ "$status" == 0 || "$status" == 1 ]] || exit "$status"
  jq -e '.SchemaVersion == 2 and (.Results | type == "array" and length > 0)
    and all(.Results[]; (.Target | type == "string") and (.Type | type == "string")
      and (.Vulnerabilities == null or (.Vulnerabilities | type == "array")))' "$report" >/dev/null
  count="$(jq '[.Results[].Vulnerabilities[]?] | length' "$report")"
  if [[ "$status" == 0 && "$count" != 0 || "$status" == 1 && "$count" == 0 ]]; then
    echo "inconsistent Trivy exit/report: $image" >&2
    exit 1
  fi
  if ! jq -e 'all(.Results[]; .Type == "gobinary" or ((.Vulnerabilities // []) | length == 0))' "$report" >/dev/null; then
    echo "non-Go vulnerability finding: $report" >&2
    exit 1
  fi
  jq -r '.Results[] | select(.Type == "gobinary" and ((.Vulnerabilities // []) | length > 0)) | .Target' "$report" >"$report_dir/$image_index.targets"
  binary_index=0
  while IFS= read -r target; do
    [[ "$target" =~ ^[A-Za-z0-9_./-]+$ && "$target" != /* && "/$target/" != *"/../"* ]] || {
      echo "unsafe Go target path: $report" >&2
      exit 1
    }
    if [[ -z "$container_id" ]]; then
      if [[ "$source" == remote ]]; then docker pull --platform "$platform" "$image"; fi
      container_id="$(docker create --platform "$platform" "$image")"
    fi
    binary_index=$((binary_index + 1))
    artifact="$report_dir/$image_index.$binary_index"
    printf '%s\n' "$image" "$target" >"$artifact.identity"
    docker cp "$container_id:/$target" "$artifact.bin"
    [[ -f "$artifact.bin" && ! -L "$artifact.bin" ]]
    sha256sum "$artifact.bin" >"$artifact.sha256"
    "$govulncheck_bin" -mode=extract "$artifact.bin" >"$artifact.extract.json"
    # stripped binary의 module-level fallback은 코드 부재의 증명이 아니다.
    jq -se 'length == 2 and .[0].name == "govulncheck-extract" and .[0].version == "0.1.0"
      and .[1].goos == "linux" and .[1].goarch == "arm64"
      and (.[1].pkgSymbols | type == "array" and length > 0)' "$artifact.extract.json" >/dev/null
    "$govulncheck_bin" -mode=binary -scan=package -format=openvex "$artifact.bin" >"$artifact.vex.json"
    grpc_release=""
    if jq -e --arg target "$target" 'any(.Results[];
      .Type == "gobinary" and .Target == $target and any(.Vulnerabilities[]?;
        .VulnerabilityID == "CVE-2026-84445"
        and .PkgIdentifier.PURL == "pkg:golang/google.golang.org/grpc@v1.84.0"))' "$report" >/dev/null; then
      grpc_release="$(go version -m "$artifact.bin" | awk '$1 == "dep" && $2 == "google.golang.org/grpc" { print $3 " " $4 }')"
    fi
    jq -e --slurpfile scan "$report" --slurpfile extract "$artifact.extract.json" \
      --arg target "$target" --arg grpc_release "$grpc_release" \
      -f "$root_dir/scripts/ci/check-go-image-vex.jq" "$artifact.vex.json" >/dev/null || {
      echo "Go finding lacks exact package-absence or fixed-release proof: $artifact.vex.json" >&2
      exit 1
    }
    echo "Go finding verified: $target (raw findings retained in $report)"
  done <"$report_dir/$image_index.targets"
  if [[ -n "$container_id" ]]; then
    docker rm "$container_id" >/dev/null
    container_id=
  fi
done <"$manifest"
[[ "$image_index" -gt 0 ]] || { echo "empty final image scan manifest" >&2; exit 1; }
