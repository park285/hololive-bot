#!/usr/bin/env bash
set -euo pipefail
[[ $# == 0 ]] || { echo 'usage: build.sh' >&2; exit 2; }
root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)"
cd "$root"
inputs=(build.sh files/append_result.go.in files/capacity_guard.go.in
  files/stack_model_version.go.in models.patch profile.json)
for file in SHA256SUMS "${inputs[@]}"; do
  [[ -f "$file" && ! -L "$file" ]] || { echo "invalid NilAway input: $file" >&2; exit 1; }
done
[[ "$(awk '{print $2}' SHA256SUMS)" == "$(printf '%s\n' "${inputs[@]}")" ]] || {
  echo 'NilAway model input manifest mismatch' >&2; exit 1;
}
sha256sum --check --strict SHA256SUMS >&2
jq -e 'keys == ["go_version", "source_sum", "source_version", "source_zip_sha256"]
  and (.source_version | test("^v[0-9]+\\.[0-9]+\\.[0-9]+-[0-9]{14}-[0-9a-f]{12}$"))
  and (.source_sum | test("^h1:[A-Za-z0-9+/]+=$"))
  and (.source_zip_sha256 | test("^[0-9a-f]{64}$"))
  and (.go_version | test("^go[0-9]+\\.[0-9]+\\.[0-9]+$"))' profile.json >/dev/null
source_version="$(jq -r .source_version profile.json)"
go_version="$(jq -r .go_version profile.json)"
[[ "${NILAWAY_VERSION:-$source_version}" == "$source_version" ]] || {
  echo 'NilAway source pin does not match the model profile' >&2; exit 1;
}
profile_id="$(sha256sum SHA256SUMS | cut -d ' ' -f1)"
export GOTOOLCHAIN="$go_version" GOWORK=off GOFLAGS=-mod=readonly CGO_ENABLED=0
GOOS="$(go env GOHOSTOS)"
GOARCH="$(go env GOHOSTARCH)"
export GOOS GOARCH
[[ "$(go env GOVERSION)" == "$go_version" ]]
cache_root="${GOBIN:-${XDG_CACHE_HOME:-$HOME/.cache}/iris-go-tools}/nilaway-models"
[[ "$cache_root" == /* ]] || { echo 'NilAway cache must be absolute' >&2; exit 1; }
umask 077
mkdir -p "$cache_root"
exec 9>"$cache_root/build.lock"
flock -w 300 9 || { echo 'NilAway build lock unavailable' >&2; exit 1; }
destination="$cache_root/$profile_id-$GOOS-$GOARCH"
verify_binary() {
  local directory="$1"
  [[ -d "$directory" && ! -L "$directory" ]]
  [[ -f "$directory/nilaway" && ! -L "$directory/nilaway" && -f "$directory/BINARY.sha256" && ! -L "$directory/BINARY.sha256" ]]
  [[ "$(awk '{print $2}' "$directory/BINARY.sha256")" == nilaway ]]
  (cd "$directory" && sha256sum --check --strict BINARY.sha256 >&2)
  [[ "$("$directory/nilaway" -stack-model-version)" == "$source_version $profile_id $go_version" ]]
}
if [[ -e "$destination" ]]; then
  verify_binary "$destination"
  printf '%s/nilaway\n' "$destination"
  exit 0
fi
temporary="$(mktemp -d "$cache_root/build.XXXXXX")"
trap 'if [[ -n "$temporary" ]]; then echo "NilAway build evidence retained: $temporary" >&2; fi' EXIT
go mod download -json "go.uber.org/nilaway@$source_version" >"$temporary/download.json"
[[ "$(jq -r .Version "$temporary/download.json")" == "$source_version" ]]
[[ "$(jq -r .Sum "$temporary/download.json")" == "$(jq -r .source_sum profile.json)" ]]
archive="$(jq -er '.Zip | select(type == "string" and length > 0)' "$temporary/download.json")"
[[ "$(sha256sum "$archive" | cut -d ' ' -f1)" == "$(jq -r .source_zip_sha256 profile.json)" ]]
# 수정될 수 있는 모듈 캐시 디렉터리 대신 검증된 아카이브를 추출한다.
unzip -q "$archive" -d "$temporary/unpacked"
source_dir="$temporary/unpacked/go.uber.org/nilaway@$source_version"
patch --batch --fuzz=0 -p1 -d "$source_dir" <models.patch >&2
install -m 0644 files/append_result.go.in "$source_dir/assertion/function/assertiontree/append_result.go"
install -m 0644 files/capacity_guard.go.in "$source_dir/assertion/function/assertiontree/capacity_guard.go"
install -m 0644 files/stack_model_version.go.in "$source_dir/cmd/nilaway/stack_model_version.go"
go -C "$source_dir" build -trimpath -buildvcs=false -o "$temporary/nilaway" \
  -ldflags="-X main.stackSourceVersion=$source_version -X main.stackModelProfile=$profile_id" ./cmd/nilaway
sha256sum --check --strict SHA256SUMS >&2
[[ "$(sha256sum SHA256SUMS | cut -d ' ' -f1)" == "$profile_id" ]]
(cd "$temporary" && sha256sum nilaway >BINARY.sha256)
verify_binary "$temporary"
mv "$temporary" "$destination"
temporary=
printf '%s/nilaway\n' "$destination"
