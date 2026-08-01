#!/usr/bin/env bash

native_release_id_validate() {
  local release_id="$1"

  if [[ ! "$release_id" =~ ^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$ ]]; then
    echo "RELEASE_ID must be one safe path component (1-128 ASCII letters, digits, dot, underscore, or hyphen; first character alphanumeric)" >&2
    return 2
  fi
}

native_release_dir_resolve() {
  local releases_root="$1"
  local release_id="$2"
  local current_link="$3"
  local root_real release_dir release_real current_real

  native_release_id_validate "$release_id" || return
  root_real="$(realpath -e -- "$releases_root" 2>/dev/null)" || {
    echo "host-native releases root is unavailable: $releases_root" >&2
    return 1
  }
  if [[ "$root_real" != "$releases_root" ]]; then
    echo "host-native releases root is not canonical: $releases_root -> $root_real" >&2
    return 1
  fi

  release_dir="$releases_root/$release_id"
  release_real="$(realpath -m -- "$release_dir" 2>/dev/null)" || {
    echo "host-native release path cannot be canonicalized: $release_dir" >&2
    return 1
  }
  case "$release_real" in
    "$root_real"/*) ;;
    *)
      echo "host-native release path escapes releases root: $release_dir -> $release_real" >&2
      return 1
      ;;
  esac

  current_real="$(realpath -e -- "$current_link" 2>/dev/null || true)"
  if [[ -n "$current_real" && "$release_real" == "$current_real" ]]; then
    echo "refusing to overwrite active host-native release: $release_dir -> $release_real" >&2
    return 1
  fi

  printf '%s\n' "$release_dir"
}
