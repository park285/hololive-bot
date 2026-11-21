#!/usr/bin/env bash
set -euo pipefail

if (( $# == 0 )); then
  echo "usage: $0 <package>..." >&2
  exit 2
fi

template='{{range .TestGoFiles}}{{$.ImportPath}}{{"\t"}}{{.}}{{"\n"}}{{end}}{{range .XTestGoFiles}}{{$.ImportPath}}{{"\t"}}{{.}}{{"\n"}}{{end}}'
nonrace_dir="$(mktemp -d)"
trap 'rm -rf -- "$nonrace_dir"' EXIT

go list -race -f "${template}" "$@" >"${nonrace_dir}/race"
go list -f "${template}" "$@" >"${nonrace_dir}/normal"
awk -F '\t' '
  NF == 2 && FILENAME == ARGV[1] { race[$0] = 1; tested[$1] = 1; next }
  NF == 2 { normal[$0] = 1; if (!($0 in race)) needed[$1] = 1 }
  END {
    for (file in race) if (!(file in normal)) {
      split(file, fields, FS)
      delete tested[fields[1]]
    }
    for (pkg in needed) print pkg, (pkg in tested ? "added" : "all")
  }
' "${nonrace_dir}/race" "${nonrace_dir}/normal" >"${nonrace_dir}/packages"

list_names() {
  if ! go test -count=1 -list '^(Test|Example|Fuzz)' "$@" >"${nonrace_dir}/list"; then
    cat "${nonrace_dir}/list" >&2
    return 1
  fi
  sed -nE '/^(Test|Example|Fuzz)[^[:space:]]*$/p' "${nonrace_dir}/list" | LC_ALL=C sort
}

while read -r package selection; do
  if [[ "${selection}" == "all" ]]; then
    go test -count=1 "${package}"
    continue
  fi

  list_names "${package}" >"${nonrace_dir}/normal-names"
  list_names -race "${package}" >"${nonrace_dir}/race-names"
  names="$(LC_ALL=C comm -23 "${nonrace_dir}/normal-names" "${nonrace_dir}/race-names" | paste -sd '|')"
  if [[ -n "${names}" ]]; then
    go test -count=1 -run "^(${names})\$" "${package}"
  fi
done <"${nonrace_dir}/packages"