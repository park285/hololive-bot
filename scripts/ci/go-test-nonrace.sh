#!/usr/bin/env bash
set -euo pipefail

if (( $# == 0 )); then
  echo "usage: $0 <package>..." >&2
  exit 2
fi

# Go가 선택한 테스트 파일을 비교해 race 빌드에서 빠지는 테스트 함수만 실행한다.
# 같은 패키지의 나머지 테스트는 race 실행이 이미 덮으므로 다시 돌리지 않는다.
# 빌드 태그·플랫폼·모듈 범위는 호출자의 go test와 같은 규칙을 따른다.
template='{{range .TestGoFiles}}{{$.ImportPath}} {{$.Dir}}/{{.}}{{"\n"}}{{end}}{{range .XTestGoFiles}}{{$.ImportPath}} {{$.Dir}}/{{.}}{{"\n"}}{{end}}'
nonrace_dir="$(mktemp -d)"
trap 'rm -rf -- "$nonrace_dir"' EXIT

# 같은 바이트 순서로 비교하고, 각 단계의 실패를 상위 게이트에 전달한다.
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
