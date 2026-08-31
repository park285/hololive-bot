#!/usr/bin/env bash
set -euo pipefail

root_dir="$(git rev-parse --show-toplevel)"
manifest="$root_dir/scripts/ci/final-image-scan-manifest.txt"

if [[ "$(trivy --version | sed -n 's/^Version: //p')" != "0.74.0" ]]; then
  echo "final image scan requires Trivy 0.74.0" >&2
  exit 1
fi

while IFS= read -r image; do
  [[ -n "$image" ]] || continue
  if ! docker image inspect "$image" >/dev/null 2>&1; then
    docker pull "$image"
  fi
  echo "trivy image: $image"
  trivy image --exit-code 1 --no-progress --scanners vuln --severity HIGH,CRITICAL "$image"
done <"$manifest"
