#!/usr/bin/env bash
set -euo pipefail
[[ $# == 0 ]] || { echo 'usage: build.sh' >&2; exit 2; }
root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)"
cd "$root"
# 제거 조건: upstream x/tools objectpath가 generic method 순서를 source와 export data에서 같게 만들고,
# 제품 분석의 source/export 경계에서 object identity가 유지되면 이 profile을 삭제한다.
inputs=(build.sh files/stack_profile_version.go.in objectpath.patch profile.json)
for file in SHA256SUMS "${inputs[@]}"; do
  [[ -f "$file" && ! -L "$file" ]] || { echo "invalid staticcheck facts input: $file" >&2; exit 1; }
done
[[ "$(awk '{print $2}' SHA256SUMS)" == "$(printf '%s\n' "${inputs[@]}")" ]] || {
  echo 'staticcheck facts input manifest mismatch' >&2; exit 1;
}
sha256sum --check --strict SHA256SUMS >&2
jq -e 'keys == ["go_version", "staticcheck_release", "staticcheck_sum", "staticcheck_version",
    "staticcheck_zip_sha256", "tools_sum", "tools_version", "tools_zip_sha256"]
  and (.staticcheck_version | test("^v[0-9]+\\.[0-9]+\\.[0-9]+$"))
  and (.staticcheck_release | test("^[0-9]{4}\\.[0-9]+\\.[0-9]+$"))
  and (.tools_version | test("^v[0-9]+\\.[0-9]+\\.[0-9]+(-0\\.[0-9]{14}-[0-9a-f]{12})?$"))
  and ([.staticcheck_sum, .tools_sum] | all(test("^h1:[A-Za-z0-9+/]+=$")))
  and ([.staticcheck_zip_sha256, .tools_zip_sha256] | all(test("^[0-9a-f]{64}$")))
  and (.go_version | test("^go[0-9]+\\.[0-9]+\\.[0-9]+$"))' profile.json >/dev/null
staticcheck_version="$(jq -r .staticcheck_version profile.json)"
staticcheck_release="$(jq -r .staticcheck_release profile.json)"
tools_version="$(jq -r .tools_version profile.json)"
go_version="$(jq -r .go_version profile.json)"
[[ "${STATICCHECK_VERSION:-$staticcheck_release}" == "$staticcheck_release" ]] || {
  echo 'staticcheck release pin does not match the facts profile' >&2; exit 1;
}
profile_id="$(sha256sum SHA256SUMS | cut -d ' ' -f1)"
identity="$staticcheck_version $tools_version $profile_id $go_version"
release_marker="staticcheck $staticcheck_release (${staticcheck_version#v})"
export GOTOOLCHAIN="$go_version" GOWORK=off GOFLAGS=-mod=readonly CGO_ENABLED=0
GOOS="$(go env GOHOSTOS)"
GOARCH="$(go env GOHOSTARCH)"
export GOOS GOARCH
[[ "$(go env GOVERSION)" == "$go_version" ]]
cache_root="${GOBIN:-${XDG_CACHE_HOME:-$HOME/.cache}/iris-go-tools}/staticcheck-facts"
[[ "$cache_root" == /* ]] || { echo 'staticcheck facts cache must be absolute' >&2; exit 1; }
umask 077
mkdir -p "$cache_root"
exec 9>"$cache_root/build.lock"
flock -w 300 9 || { echo 'staticcheck facts build lock unavailable' >&2; exit 1; }
destination="$cache_root/$profile_id-$GOOS-$GOARCH"
verify_binary() {
  local directory="$1"
  [[ -d "$directory" && ! -L "$directory" ]]
  [[ -f "$directory/staticcheck" && ! -L "$directory/staticcheck" && -f "$directory/BINARY.sha256" && ! -L "$directory/BINARY.sha256" ]]
  [[ "$(awk '{print $2}' "$directory/BINARY.sha256")" == staticcheck ]]
  (cd "$directory" && sha256sum --check --strict BINARY.sha256 >&2)
  [[ "$("$directory/staticcheck" -stack-profile-version)" == "$identity" ]]
  [[ "$("$directory/staticcheck" -version)" == "$release_marker" ]]
}
if [[ -e "$destination" ]]; then
  verify_binary "$destination"
  printf '%s/staticcheck\n' "$destination"
  exit 0
fi
temporary="$(mktemp -d "$cache_root/build.XXXXXX")"
trap 'if [[ -n "$temporary" ]]; then echo "staticcheck facts build evidence retained: $temporary" >&2; fi' EXIT
# module@version, sumdb hash, zip sha256을 모두 profile과 대조한 archive만 추출한다.
verified_archive() {
  local name="$1" module="$2" version="$3" metadata="$temporary/$1-download.json"
  go mod download -json "$module@$version" >"$metadata"
  [[ "$(jq -r .Version "$metadata")" == "$version" ]]
  [[ "$(jq -r .Sum "$metadata")" == "$(jq -r ".${name}_sum" profile.json)" ]] || {
    echo "$module@$version module sum mismatch" >&2; exit 1;
  }
  archive="$(jq -er '.Zip | select(type == "string" and length > 0)' "$metadata")"
  [[ "$(sha256sum "$archive" | cut -d ' ' -f1)" == "$(jq -r ".${name}_zip_sha256" profile.json)" ]] || {
    echo "$module@$version zip sha256 mismatch" >&2; exit 1;
  }
  # 수정됐을 수 있는 module-cache 디렉터리 대신 검증한 archive byte를 추출한다.
  unzip -q "$archive" -d "$temporary/unpacked"
}
verified_archive staticcheck honnef.co/go/tools "$staticcheck_version"
verified_archive tools golang.org/x/tools "$tools_version"
source_dir="$temporary/unpacked/honnef.co/go/tools@$staticcheck_version"
tools_dir="$temporary/unpacked/golang.org/x/tools@$tools_version"
# Go 1.27.2의 export data v5를 읽는 x/tools를 명시적으로 선택한다.
# source/export generic method 순서 보정은 이 고정 소스에만 적용한다.
patch --batch --fuzz=0 -p1 -d "$tools_dir" <objectpath.patch >&2
install -m 0644 files/stack_profile_version.go.in "$source_dir/cmd/staticcheck/stack_profile_version.go"
go -C "$source_dir" mod edit -require "golang.org/x/tools@$tools_version" -replace "golang.org/x/tools=$tools_dir"
go -C "$source_dir" mod tidy
go -C "$source_dir" build -trimpath -buildvcs=false -o "$temporary/staticcheck" \
  -ldflags="-X main.stackStaticcheckVersion=$staticcheck_version -X main.stackToolsVersion=$tools_version -X main.stackFactsProfile=$profile_id" \
  ./cmd/staticcheck
sha256sum --check --strict SHA256SUMS >&2
[[ "$(sha256sum SHA256SUMS | cut -d ' ' -f1)" == "$profile_id" ]]
(cd "$temporary" && sha256sum staticcheck >BINARY.sha256)
verify_binary "$temporary"
mv "$temporary" "$destination"
temporary=
printf '%s/staticcheck\n' "$destination"
