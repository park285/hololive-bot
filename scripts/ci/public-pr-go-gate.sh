#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"

usage() {
  echo "usage: $0 <module> <tidy|vet|test|race>" >&2
  exit 2
}

(( $# == 2 )) || usage
module="$1"
stage="$2"

case "${module}" in
  .|admin-dashboard/backend|hololive/hololive-api|hololive/hololive-alarm-worker|hololive/hololive-dbtest|hololive/hololive-shared|hololive/hololive-youtube-producer)
    ;;
  *)
    echo "unsupported public PR module: ${module}" >&2
    exit 2
    ;;
esac

case "${stage}" in
  tidy|vet|test|race)
    ;;
  *)
    echo "unsupported public PR stage: ${stage}" >&2
    exit 2
    ;;
esac

module_dir="${ROOT_DIR}"
if [[ "${module}" != "." ]]; then
  module_dir="${ROOT_DIR}/${module}"
fi

[[ -f "${module_dir}/go.mod" ]] || {
  echo "go.mod is missing for public PR module: ${module}" >&2
  exit 1
}

export GOWORK=off
export GOMAXPROCS="${GOMAXPROCS:-2}"
export GOMEMLIMIT="${GOMEMLIMIT:-5GiB}"

cd "${module_dir}"

case "${stage}" in
  tidy)
    echo "[public-pr] module=${module} go mod tidy -diff"
    go mod tidy -diff
    ;;
  vet)
    export GOFLAGS="${GOFLAGS:+${GOFLAGS} }-mod=readonly"
    echo "[public-pr] module=${module} go vet ./..."
    go vet ./...
    ;;
  test)
    export GOFLAGS="${GOFLAGS:+${GOFLAGS} }-mod=readonly"
    echo "[public-pr] module=${module} go test -count=1 ./..."
    go test -count=1 ./...
    ;;
  race)
    export GOFLAGS="${GOFLAGS:+${GOFLAGS} }-mod=readonly"
    echo "[public-pr] module=${module} go test -race -p 2 -count=1 ./..."
    go test -race -p 2 -count=1 ./...
    ;;
esac
