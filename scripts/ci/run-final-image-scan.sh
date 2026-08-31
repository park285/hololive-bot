#!/usr/bin/env bash
set -euo pipefail

root_dir="$(git rev-parse --show-toplevel)"
manifest="$root_dir/scripts/ci/final-image-scan-manifest.txt"
nginx_exception_target='remote|linux/arm64|nginx:1.31.4-alpine-slim@sha256:1870de6d59aafee152589b64404556d2535922cdd998e6dac1c4888c938ed8f9'
postgres_exception_target='remote|linux/arm64|postgres:18.6-alpine@sha256:d3e1620b530c944afa6e887d22eb899824da68e19c52024bf98f5220c88a65b2'
deunhealth_exception_target='remote|linux/arm64|qmcgaw/deunhealth@sha256:db1e4fcd3aceeb0da34a83f7a8a5432df586e6d0388ddb6ad8dd7b479e4aa25d'
socket_proxy_exception_target='remote|linux/arm64|wollomatic/socket-proxy:1.12.3@sha256:74e770f5ed3cfc9ecb6350e177d2aa55873568c85bc953079834e68607dbf71b'

if [[ "$(trivy --version | sed -n 's/^Version: //p')" != "0.74.0" ]]; then
  echo "final image scan requires Trivy 0.74.0" >&2
  exit 1
fi

while IFS='|' read -r source platform image; do
  [[ -n "$source" && -n "$platform" && -n "$image" ]] || continue
  scan_args=(image --exit-code 1 --no-progress --scanners vuln --severity "HIGH,CRITICAL")
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
  ignore_file=
  case "$source|$platform|$image" in
    "$nginx_exception_target")
      ignore_file="$root_dir/scripts/ci/trivyignore-nginx.yaml"
      ;;
    "$postgres_exception_target")
      ignore_file="$root_dir/scripts/ci/trivyignore-postgres.yaml"
      ;;
    "$deunhealth_exception_target")
      ignore_file="$root_dir/scripts/ci/trivyignore-deunhealth.yaml"
      ;;
    "$socket_proxy_exception_target")
      ignore_file="$root_dir/scripts/ci/trivyignore-socket-proxy.yaml"
      ;;
  esac
  [[ -z "$ignore_file" ]] || scan_args+=(--ignorefile "$ignore_file")
  trivy "${scan_args[@]}" "$image"
done <"$manifest"
