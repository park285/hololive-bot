#!/usr/bin/env bash
set -euo pipefail

if (( $# == 0 )); then
  echo "usage: $0 <package>..." >&2
  exit 2
fi

# Go가 선택한 테스트 파일을 비교해 race 빌드에서 빠지는 패키지만 실행한다.
# 빌드 태그·플랫폼·모듈 범위는 호출자의 go test와 같은 규칙을 따른다.
template='{{range .TestGoFiles}}{{$.ImportPath}} {{.}}{{"\n"}}{{end}}{{range .XTestGoFiles}}{{$.ImportPath}} {{.}}{{"\n"}}{{end}}'
normal_files="$(go list -f "${template}" "$@")"
race_files="$(go list -race -f "${template}" "$@")"
mapfile -t packages < <(
  comm -23 <(printf '%s\n' "${normal_files}" | sort -u) <(printf '%s\n' "${race_files}" | sort -u) \
    | awk 'NF {print $1}' | sort -u
)
if (( ${#packages[@]} > 0 )); then
  go test -count=1 "${packages[@]}"
fi
