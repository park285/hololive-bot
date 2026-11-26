#!/usr/bin/env bash
set -euo pipefail

normal_env=()
test_flags=()
while [[ "${1:-}" == --* ]]; do
  case "$1" in
    --cgo-disabled) normal_env=(env CGO_ENABLED=0) ;;
    --json) test_flags=(-json) ;;
    *) echo "unsupported option: $1" >&2; exit 2 ;;
  esac
  shift
done

if (( $# == 0 )); then
  echo "usage: $0 [--cgo-disabled] [--json] <package>..." >&2
  exit 2
fi

template='{{range .GoFiles}}{{$.ImportPath}}{{"\tsource\t"}}{{.}}{{"\n"}}{{end}}{{range .CgoFiles}}{{$.ImportPath}}{{"\tsource\t"}}{{.}}{{"\n"}}{{end}}{{range .TestGoFiles}}{{$.ImportPath}}{{"\ttest\t"}}{{.}}{{"\n"}}{{end}}{{range .XTestGoFiles}}{{$.ImportPath}}{{"\ttest\t"}}{{.}}{{"\n"}}{{end}}'
nonrace_dir="$(mktemp -d)"
trap 'rm -rf -- "$nonrace_dir"' EXIT

go list -race -f "${template}" "$@" >"${nonrace_dir}/race"
"${normal_env[@]}" go list -f "${template}" "$@" >"${nonrace_dir}/normal"
awk -F '\t' '
  NF == 3 && FILENAME == ARGV[1] { race[$0] = 1; tested[$1] = 1; next }
  NF == 3 {
    normal[$0] = 1
    if (!($0 in race)) {
      needed[$1] = 1
      if ($2 == "source") full[$1] = 1
    }
  }
  END {
    for (file in race) if (!(file in normal)) {
      split(file, fields, FS)
      full[fields[1]] = 1
      if (fields[2] == "source") needed[fields[1]] = 1
    }
    for (pkg in needed) print pkg, (pkg in tested && !(pkg in full) ? "added" : "all")
  }
' "${nonrace_dir}/race" "${nonrace_dir}/normal" >"${nonrace_dir}/packages"

list_names() {
  local mode="$1"
  local -a mode_env=()
  shift
  [[ "${mode}" != normal ]] || mode_env=("${normal_env[@]}")
  if ! "${mode_env[@]}" go test -count=1 -list '^(Test|Example|Fuzz)' "$@" >"${nonrace_dir}/list"; then
    cat "${nonrace_dir}/list" >&2
    return 1
  fi
  sed -nE '/^(Test|Example|Fuzz)[^[:space:]]*$/p' "${nonrace_dir}/list" | LC_ALL=C sort
}

while read -r package selection; do
  if [[ "${selection}" == "all" ]]; then
    "${normal_env[@]}" go test "${test_flags[@]}" "${package}"
    continue
  fi

  list_names normal "${package}" >"${nonrace_dir}/normal-names"
  list_names race -race "${package}" >"${nonrace_dir}/race-names"
  names="$(LC_ALL=C comm -23 "${nonrace_dir}/normal-names" "${nonrace_dir}/race-names" | paste -sd '|')"
  if [[ -n "${names}" ]]; then
    "${normal_env[@]}" go test "${test_flags[@]}" -run "^(${names})\$" "${package}"
  fi
done <"${nonrace_dir}/packages"
