#!/usr/bin/env bash
set -euo pipefail

if (( $# == 0 )); then
  echo "usage: $0 <package>..." >&2
  exit 2
fi

template='{{range .TestGoFiles}}{{$.ImportPath}} {{$.Dir}}/{{.}}{{"\n"}}{{end}}{{range .XTestGoFiles}}{{$.ImportPath}} {{$.Dir}}/{{.}}{{"\n"}}{{end}}'
nonrace_dir="$(mktemp -d)"
trap 'rm -rf -- "$nonrace_dir"' EXIT

go list -f "${template}" "$@" | LC_ALL=C sort -u >"${nonrace_dir}/normal"
go list -race -f "${template}" "$@" | LC_ALL=C sort -u >"${nonrace_dir}/race"
LC_ALL=C comm -23 "${nonrace_dir}/normal" "${nonrace_dir}/race" >"${nonrace_dir}/nonrace-only"

declare -A tests_by_package=()
while read -r package file; do
  [[ -n "${package}" ]] || continue
  while IFS= read -r name; do
    tests_by_package["${package}"]+="${tests_by_package[${package}]:+|}${name}"
  done < <(sed -nE 's/^func (Test[A-Za-z0-9_]*)\(.*/\1/p' "${file}")
done <"${nonrace_dir}/nonrace-only"

for package in "${!tests_by_package[@]}"; do
  go test -count=1 -run "^(${tests_by_package[${package}]})\$" "${package}"
done
