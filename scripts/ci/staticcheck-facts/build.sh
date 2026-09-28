#!/usr/bin/env bash
set -euo pipefail
[[ $# == 0 ]] || { echo 'usage: build.sh' >&2; exit 2; }
root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)"
cd "$root"
# 제거 조건: upstream x/tools objectpath가 generic method 순서를 source와 export data에서 같게 만들고,
# 이 profile의 source/export 경계 fixture가 무패치 staticcheck로도 통과하면 이 profile을 삭제한다.
inputs=(build.sh files/deprecation-api.go.in files/deprecation-deprecated.go.in
  files/deprecation-healthy.go.in files/objectpath_order_test.go.in files/stack_profile_version.go.in
  files/upstream-protobuf-pkg.go.in
  objectpath.patch profile.json)
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
  and (.tools_version | test("^v[0-9]+\\.[0-9]+\\.[0-9]+-0\\.[0-9]{14}-[0-9a-f]{12}$"))
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
# 패치는 staticcheck가 원래 선택하는 x/tools revision에만 적용하며 revision 자체는 바꾸지 않는다.
[[ "$(go -C "$source_dir" mod edit -json |
  jq -r '[.Require[] | select(.Path == "golang.org/x/tools") | .Version] | join(" ")')" == "$tools_version" ]] || {
  echo 'staticcheck no longer requires the pinned x/tools revision' >&2; exit 1;
}
grep -Fqx "golang.org/x/tools $tools_version $(jq -r .tools_sum profile.json)" "$source_dir/go.sum" || {
  echo 'staticcheck go.sum does not bind the pinned x/tools sum' >&2; exit 1;
}
patch --batch --fuzz=0 -p1 -d "$tools_dir" <objectpath.patch >&2
install -m 0644 files/objectpath_order_test.go.in "$tools_dir/go/types/objectpath/objectpath_order_test.go"
install -m 0644 files/stack_profile_version.go.in "$source_dir/cmd/staticcheck/stack_profile_version.go"
go -C "$source_dir" mod edit -replace "golang.org/x/tools=$tools_dir"
GOMEMLIMIT=4GiB GOMAXPROCS=4 go -C "$tools_dir" test -count=1 -p 1 ./go/types/objectpath 2>&1 |
  tee "$temporary/tools-tests.log" >&2
# objectpath를 소비하는 runner fact cache, deprecated fact, SA1019, unused serialization 경로를 검증한다.
# testutil의 go.mod overlay를 디스크에도 동일하게 둔다. loader/hash.go는
# overlay 대신 실제 파일을 해시하므로 archive 기반 테스트에서 go1.0이 실패한다.
# 분석기 코드나 fixture의 언어 버전·기대 진단은 변경하지 않는다.
for fixture in "$source_dir/staticcheck/sa1019/testdata"/go*; do
  printf 'module example.com\ngo %s' "${fixture##*/go}" >"$fixture/go.mod"
done
# Go module ZIP은 vendor를 제외한다. v0.8.1의 원본 fixture를 byte 그대로 복원한다.
# https://github.com/dominikh/go-tools/blob/v0.8.1/staticcheck/sa1019/testdata/go1.0/vendor/github.com/golang/protobuf/proto/pkg.go
# Git blob: 1d0160b61c40e120c49534e9b1e9067dde58da33
install -D -m 0644 files/upstream-protobuf-pkg.go.in \
  "$source_dir/staticcheck/sa1019/testdata/go1.0/vendor/github.com/golang/protobuf/proto/pkg.go"
GOMEMLIMIT=4GiB GOMAXPROCS=4 go -C "$source_dir" test -count=1 -p 1 -parallel=2 \
  ./analysis/facts/deprecated ./lintcmd/... ./staticcheck/sa1019 ./unused 2>&1 |
  tee "$temporary/staticcheck-tests.log" >&2
go -C "$source_dir" build -trimpath -buildvcs=false -o "$temporary/staticcheck" \
  -ldflags="-X main.stackStaticcheckVersion=$staticcheck_version -X main.stackToolsVersion=$tools_version -X main.stackFactsProfile=$profile_id" \
  ./cmd/staticcheck
# 실제 CLI로 source에서 만든 deprecated fact를 export data로 읽은 호출자에 적용한다.
# 빈 staticcheck cache를 써서 이전 binary의 fact를 재사용하지 않는다.
for fixture in healthy deprecated; do
  fixture_dir="$temporary/deprecation-contract/$fixture"
  install -D -m 0644 files/deprecation-api.go.in "$fixture_dir/api/api.go"
  install -D -m 0644 "files/deprecation-$fixture.go.in" "$fixture_dir/consumer/consumer.go"
  printf 'module example.com/staticcheck-contract\n\ngo %s\n' "${go_version#go}" >"$fixture_dir/go.mod"
  if (cd "$fixture_dir" && STATICCHECK_CACHE="$fixture_dir/staticcheck-cache" \
      "$temporary/staticcheck" -checks SA1019 ./consumer) >"$fixture_dir/analysis.log" 2>&1; then
    status=0
  else
    status=$?
  fi
  if [[ "$fixture" == healthy ]]; then
    [[ "$status" == 0 && ! -s "$fixture_dir/analysis.log" ]] || {
      cat "$fixture_dir/analysis.log" >&2; echo 'healthy method call reported as deprecated' >&2; exit 1;
    }
  else
    [[ "$status" == 1 && "$(grep -c '(SA1019)$' "$fixture_dir/analysis.log")" == 1 &&
      "$(grep -Ec '^[^:]*consumer\.go:6:[0-9]+: .*Legacy is deprecated: use Healthy instead\. *\(SA1019\)$' \
        "$fixture_dir/analysis.log")" == 1 ]] || {
      cat "$fixture_dir/analysis.log" >&2; echo 'deprecated method call lost its SA1019 warning' >&2; exit 1;
    }
  fi
  printf 'deprecation fact contract: %s passed\n' "$fixture" >&2
done
sha256sum --check --strict SHA256SUMS >&2
[[ "$(sha256sum SHA256SUMS | cut -d ' ' -f1)" == "$profile_id" ]]
(cd "$temporary" && sha256sum staticcheck >BINARY.sha256)
verify_binary "$temporary"
mv "$temporary" "$destination"
temporary=
printf '%s/staticcheck\n' "$destination"
