#!/usr/bin/env bash

youtubejs_node_version_supported() {
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
  if (( 10#$major == 22 )); then
    (( 10#$minor > 22 || (10#$minor == 22 && 10#$patch >= 2) ))
    return
  fi
  if (( 10#$major == 24 )); then
    (( 10#$minor > 15 || (10#$minor == 15 && 10#$patch >= 0) ))
    return
  fi
  (( 10#$major >= 26 ))
}

require_youtubejs_node_version() {
  local node_path="${1:-node}"
  local version=""

  command -v "$node_path" >/dev/null 2>&1 || {
    echo "YouTube.js Node runtime is unavailable: $node_path" >&2
    return 1
  }
  version="$("$node_path" --version)"
  youtubejs_node_version_supported "$version" || {
    echo "YouTube.js Node runtime $version does not satisfy ^22.22.2 || ^24.15.0 || >=26.0.0" >&2
    return 1
  }
}
