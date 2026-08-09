#!/usr/bin/env bash

require_libpq_service() {
  if [[ -n "${PGPASSWORD:-}" ]]; then
    echo "PGPASSWORD is forbidden; use the file-only PGPASSFILE secret contract" >&2
    return 2
  fi
  if [[ -z "${PGSERVICE:-}" ]]; then
    echo "PGSERVICE is required; use a libpq service entry instead of a connection URI" >&2
    return 2
  fi
  if [[ -z "${PGPASSFILE:-}" ]]; then
    echo "PGPASSFILE is required by the file-only secret contract" >&2
    return 2
  fi
  if [[ -L "${PGPASSFILE}" ]]; then
    echo "PGPASSFILE must not be a symlink" >&2
    return 2
  fi
  if [[ ! -f "${PGPASSFILE}" || ! -r "${PGPASSFILE}" ]]; then
    echo "PGPASSFILE is not readable" >&2
    return 2
  fi
}
