#!/usr/bin/env bash
set -euo pipefail

if (( $# == 0 )); then
  echo "usage: $0 <package>..." >&2
  exit 2
fi

# Go가 선택한 테스트 파일을 비교해 race 빌드에서 빠지는 패키지만 실행한다.
# 빌드 태그·플랫폼·모듈 범위는 호출자의 go test와 같은 규칙을 따른다.
template='{{range .TestGoFiles}}{{$.ImportPath}} {{.}}{{"\n"}}{{end}}{{range .XTestGoFiles}}{{$.ImportPath}} {{.}}{{"\n"}}{{end}}'
nonrace_dir="$(mktemp -d)"
trap 'rm -rf -- "$nonrace_dir"' EXIT

# 같은 바이트 순서로 비교하고, 각 단계의 실패를 상위 게이트에 전달한다.
go list -f "${template}" "$@" | LC_ALL=C sort -u >"${nonrace_dir}/normal"
go list -race -f "${template}" "$@" | LC_ALL=C sort -u >"${nonrace_dir}/race"
selected_packages="$(LC_ALL=C comm -23 "${nonrace_dir}/normal" "${nonrace_dir}/race" | awk 'NF {print $1}' | LC_ALL=C sort -u)"
if [[ -n "${selected_packages}" ]]; then
  mapfile -t packages <<<"${selected_packages}"
  go test -count=1 "${packages[@]}"
fi
