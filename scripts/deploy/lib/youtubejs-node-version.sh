#!/usr/bin/env bash

node_version_supported() {
  local version="${1#v}"
  local major=""
  local minor=""
  local patch=""

  if [[ ! "$version" =~ ^([0-9]+)\.([0-9]+)\.([0-9]+)$ ]]; then
    return 1
  fi
  major="${BASH_REMATCH[1]}"
  minor="${BASH_REMATCH[2]}"
  patch="${BASH_REMATCH[3]}"
  (( 10#$major == 24 && (10#$minor > 20 || (10#$minor == 20 && 10#$patch >= 0)) ))
}

require_node_version() {
  local node_path="${1:-node}"
  local version=""

  command -v "$node_path" >/dev/null 2>&1 || {
    echo "YouTube.js Node runtime is unavailable: $node_path" >&2
    return 1
  }
  version="$("$node_path" --version)"
  node_version_supported "$version" || {
    echo "Node runtime $version does not satisfy ^24.20.0" >&2
    return 1
  }
}
