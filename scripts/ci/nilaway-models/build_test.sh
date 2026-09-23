#!/usr/bin/env bash
set -euo pipefail
source_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)"
work="$(mktemp -d)"
trap 'rm -rf -- "$work"' EXIT
cp -R "$source_dir" "$work/source"
mkdir -p "$work/bin" "$work/gobin"
export GOBIN="$work/gobin" PATH="$work/bin:$PATH" NILAWAY_TEST_GO_LOG="$work/go.log" NILAWAY_TEST_EXEC_LOG="$work/exec.log"
export NILAWAY_TEST_GO_VERSION
NILAWAY_TEST_GO_VERSION="$(jq -r .go_version "$source_dir/profile.json")"
version="$(jq -r .source_version "$source_dir/profile.json")"
profile="$(sha256sum "$source_dir/SHA256SUMS" | cut -d ' ' -f1)"
export NILAWAY_TEST_IDENTITY="$version $profile $NILAWAY_TEST_GO_VERSION"
cat >"$work/bin/go" <<'GO'
#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >>"$NILAWAY_TEST_GO_LOG"
case "$*" in
  'env GOHOSTOS') echo linux ;;
  'env GOHOSTARCH') echo amd64 ;;
  'env GOVERSION') echo "$NILAWAY_TEST_GO_VERSION" ;;
  *) echo 'fixture forbids builds and downloads' >&2; exit 99 ;;
esac
GO
chmod +x "$work/bin/go"
cache="$GOBIN/nilaway-models/$profile-linux-amd64"
mkdir -p "$cache"
cat >"$cache/nilaway" <<'BINARY'
#!/usr/bin/env bash
set -euo pipefail
[[ "$*" == -stack-model-version ]]
echo executed >>"$NILAWAY_TEST_EXEC_LOG"
printf '%s\n' "$NILAWAY_TEST_IDENTITY"
BINARY
chmod +x "$cache/nilaway"
(cd "$cache" && sha256sum nilaway >BINARY.sha256)
[[ "$(bash "$work/source/build.sh")" == "$cache/nilaway" ]]
expect_failure() {
  local label="$1"
  shift
  if "$@" >"$work/failure.log" 2>&1; then
    echo "FAIL: $label accepted" >&2; exit 1
  fi
  echo "PASS: $label rejected"
}
expect_failure 'source version mismatch' env NILAWAY_VERSION=v0.0.0-wrong bash "$work/source/build.sh"
expect_failure 'cached model identity mismatch' env NILAWAY_TEST_IDENTITY=wrong bash "$work/source/build.sh"
executions="$(wc -l <"$NILAWAY_TEST_EXEC_LOG")"
printf '\n' >>"$cache/nilaway"
expect_failure 'cached binary corruption' bash "$work/source/build.sh"
[[ "$(wc -l <"$NILAWAY_TEST_EXEC_LOG")" == "$executions" ]]
printf '\n' >>"$work/source/profile.json"
expect_failure 'source input corruption' bash "$work/source/build.sh"
if grep -Evq '^env (GOHOSTOS|GOHOSTARCH|GOVERSION)$' "$NILAWAY_TEST_GO_LOG"; then
  echo 'invalid cached artifacts must not trigger a replacement build' >&2; exit 1
fi
echo 'NilAway build identity and fail-closed cache contracts passed'
