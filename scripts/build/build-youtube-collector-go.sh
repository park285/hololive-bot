#!/bin/sh
set -eu

usage() {
  echo "usage: $0 --output-dir <abs> --version <non-empty> --revision <40-lower-hex> --goos linux --goarch amd64 --goamd64 v1" >&2
  echo "  optional: --allow-unknown-revision (local/dev only; accepts revision=unknown)" >&2
  exit 2
}

output_dir=
version=
revision=
goos=
goarch=
goamd64=
allow_unknown=0

while [ $# -gt 0 ]; do
  case "$1" in
    --output-dir)
      [ $# -ge 2 ] || usage
      output_dir=$2
      shift 2
      ;;
    --version)
      [ $# -ge 2 ] || usage
      version=$2
      shift 2
      ;;
    --revision)
      [ $# -ge 2 ] || usage
      revision=$2
      shift 2
      ;;
    --goos)
      [ $# -ge 2 ] || usage
      goos=$2
      shift 2
      ;;
    --goarch)
      [ $# -ge 2 ] || usage
      goarch=$2
      shift 2
      ;;
    --goamd64)
      [ $# -ge 2 ] || usage
      goamd64=$2
      shift 2
      ;;
    --allow-unknown-revision)
      allow_unknown=1
      shift
      ;;
    -h|--help)
      usage
      ;;
    *)
      echo "unknown argument: $1" >&2
      usage
      ;;
  esac
done

[ -n "${output_dir}" ] && [ -n "${version}" ] && [ -n "${revision}" ] && [ -n "${goos}" ] && [ -n "${goarch}" ] && [ -n "${goamd64}" ] || usage

case "${output_dir}" in
  /*) ;;
  *)
    echo "output-dir must be an absolute path" >&2
    exit 2
    ;;
esac

case "${version}" in
  *[!A-Za-z0-9._+-]*|'')
    echo "version must contain only ASCII letters, digits, dot, underscore, plus, or hyphen" >&2
    exit 2
    ;;
esac

revision_ok=0
case "${revision}" in
  *[!0-9a-f]*) ;;
  *)
    if [ "${#revision}" -eq 40 ]; then
      revision_ok=1
    fi
    ;;
esac
if [ "${revision_ok}" -eq 0 ]; then
  if [ "${allow_unknown}" -eq 1 ] && [ "${revision}" = "unknown" ]; then
    revision_ok=1
  fi
fi
if [ "${revision_ok}" -eq 0 ]; then
  echo "revision must be 40 lowercase hex digits" >&2
  exit 2
fi

case "${goos}" in
  *[!a-z0-9]*|'')
    echo "goos is invalid" >&2
    exit 2
    ;;
esac
case "${goarch}" in
  *[!a-z0-9]*|'')
    echo "goarch is invalid" >&2
    exit 2
    ;;
esac
case "${goamd64}" in
  *[!a-z0-9]*|'')
    echo "goamd64 is invalid" >&2
    exit 2
    ;;
esac

script_dir=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
root_dir=$(CDPATH='' cd -- "${script_dir}/../.." && pwd)
collector_dir="${root_dir}/hololive/hololive-youtube-collector"
[ -f "${collector_dir}/go.mod" ] || {
  echo "collector module is missing: ${collector_dir}" >&2
  exit 1
}

command -v sha256sum >/dev/null 2>&1 || {
  echo "sha256sum is required" >&2
  exit 1
}

tmp_dir=$(mktemp -d)
trap 'rm -rf "${tmp_dir}"' EXIT

echo "[collector-build] GOWORK=off CGO_ENABLED=0 GOOS=${goos} GOARCH=${goarch} GOAMD64=${goamd64} pgo=off json_codec=go-json-v2 version=${version} revision=${revision}"

collector_build_id="youtube-collector/${version}/${revision}"
healthcheck_build_id="healthcheck/${version}/${revision}"

export GOWORK=off
export CGO_ENABLED=0
export GOOS="${goos}"
export GOARCH="${goarch}"
export GOAMD64="${goamd64}"

(
  cd "${collector_dir}"
  go build -pgo=off -trimpath -buildvcs=false \
    -ldflags="-s -w -buildid=${collector_build_id} -X main.Version=${version} -X main.Revision=${revision}" \
    -o "${tmp_dir}/youtube-collector" ./cmd/runtime/youtube-collector
  go build -pgo=off -trimpath -buildvcs=false \
    -ldflags="-s -w -buildid=${healthcheck_build_id}" \
    -o "${tmp_dir}/healthcheck" ./cmd/runtime/healthcheck
)

mkdir -p "${output_dir}/bin"

install_file() {
  src=$1
  dest=$2
  mode=$3
  dest_dir=$(dirname -- "${dest}")
  mkdir -p "${dest_dir}"
  tmp_dest=$(mktemp "${dest_dir}/.tmp.XXXXXX")
  cp "${src}" "${tmp_dest}"
  chmod "${mode}" "${tmp_dest}"
  mv -f "${tmp_dest}" "${dest}"
}

install_file "${tmp_dir}/youtube-collector" "${output_dir}/bin/youtube-collector" 0755
install_file "${tmp_dir}/healthcheck" "${output_dir}/bin/healthcheck" 0755

collector_sha=$(sha256sum "${output_dir}/bin/youtube-collector")
collector_sha=${collector_sha%% *}
healthcheck_sha=$(sha256sum "${output_dir}/bin/healthcheck")
healthcheck_sha=${healthcheck_sha%% *}

printf '%s\n' \
  '{' \
  '  "schema_version": 1,' \
  "  \"source_revision\": \"${revision}\"," \
  "  \"version\": \"${version}\"," \
  '  "go": {' \
  '    "cgo_enabled": false,' \
  "    \"goos\": \"${goos}\"," \
  "    \"goarch\": \"${goarch}\"," \
  "    \"goamd64\": \"${goamd64}\"," \
  '    "pgo": "off",' \
  '    "tags": []' \
  '  },' \
  '  "binaries": {' \
  '    "bin/youtube-collector": {' \
  '      "package": "github.com/kapu/hololive-youtube-collector/cmd/runtime/youtube-collector",' \
  "      \"build_id\": \"${collector_build_id}\"," \
  '      "tags": []' \
  '    },' \
  '    "bin/healthcheck": {' \
  '      "package": "github.com/kapu/hololive-youtube-collector/cmd/runtime/healthcheck",' \
  "      \"build_id\": \"${healthcheck_build_id}\"," \
  '      "tags": []' \
  '    }' \
  '  },' \
  '  "files": {' \
  "    \"bin/youtube-collector\": \"${collector_sha}\"," \
  "    \"bin/healthcheck\": \"${healthcheck_sha}\"" \
  '  }' \
  '}' >"${tmp_dir}/manifest.json"

printf '%s  %s\n' "${collector_sha}" "bin/youtube-collector" >"${tmp_dir}/sha256sums.txt"
printf '%s  %s\n' "${healthcheck_sha}" "bin/healthcheck" >>"${tmp_dir}/sha256sums.txt"

install_file "${tmp_dir}/manifest.json" "${output_dir}/manifest.json" 0644
install_file "${tmp_dir}/sha256sums.txt" "${output_dir}/sha256sums.txt" 0644
