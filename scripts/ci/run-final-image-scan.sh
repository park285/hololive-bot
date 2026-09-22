#!/usr/bin/env bash
set -euo pipefail

root_dir="$(git rev-parse --show-toplevel)"
manifest="$root_dir/scripts/ci/final-image-scan-manifest.txt"

if [[ "$(trivy --version | sed -n 's/^Version: //p')" != "0.74.0" ]]; then
  echo "final image scan requires Trivy 0.74.0" >&2
  exit 1
fi

while IFS='|' read -r source platform image; do
  [[ -n "$source" && -n "$platform" && -n "$image" ]] || continue
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
      ;;
    remote)
      scan_args+=(--image-src remote --platform "$platform")
      ;;
    *)
      echo "unsupported final image scan source: $source" >&2
      exit 1
      ;;
  esac
  echo "trivy image: $source $platform $image"
  trivy "${scan_args[@]}" "$image"
done <"$manifest"
