#!/usr/bin/env bash
set -euo pipefail

umask 077

if [[ $# -ne 1 ]]; then
  echo "usage: $0 <iris-base-url>" >&2
  exit 1
fi

base_url="${1%/}"

if [[ ! "$base_url" =~ ^https?://[^[:space:]]+$ ]]; then
  echo "error: iris base url must match http[s]://host[:port][/path]" >&2
  exit 1
fi

iris_transport="$(printf '%s' "${IRIS_TRANSPORT:-h3}" | tr '[:upper:]' '[:lower:]' | xargs)"
case "$iris_transport" in
  h3|http3|http/3|quic)
    iris_transport="h3"
    ;;
  h2|http2)
    iris_transport="http2"
    ;;
  h2c)
    ;;
  http|http1|http/1.1)
    iris_transport="http1"
    ;;
esac

case "$iris_transport" in
  h3|http2)
    if [[ ! "$base_url" =~ ^https:// ]]; then
      echo "error: IRIS_TRANSPORT=$iris_transport requires https iris base url" >&2
      exit 1
    fi
    ;;
  h2c)
    if [[ ! "$base_url" =~ ^http:// ]]; then
      echo "error: IRIS_TRANSPORT=h2c requires http iris base url" >&2
      exit 1
    fi
    ;;
esac

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
runtime_dir="${RUNTIME_CONFIG_DIR:-$repo_root/runtime-config}"
target_file="${IRIS_BASE_URL_FILE:-$runtime_dir/iris_base_url}"

mkdir -p "$runtime_dir"
tmp_file="$(mktemp "$runtime_dir/.iris_base_url.XXXXXX")"
printf '%s\n' "$base_url" > "$tmp_file"
chmod 0644 "$tmp_file"
mv "$tmp_file" "$target_file"

echo "updated $target_file"
echo "current iris base url: $base_url"
