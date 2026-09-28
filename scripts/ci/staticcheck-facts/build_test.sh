#!/usr/bin/env bash
set -euo pipefail
source_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)"
work="$(mktemp -d)"
trap 'rm -rf -- "$work"' EXIT
cp -R "$source_dir" "$work/source"
mkdir -p "$work/bin" "$work/gobin"
export GOBIN="$work/gobin" PATH="$work/bin:$PATH" STATICCHECK_TEST_GO_LOG="$work/go.log" STATICCHECK_TEST_EXEC_LOG="$work/exec.log"
export STATICCHECK_TEST_GO_VERSION STATICCHECK_TEST_IDENTITY STATICCHECK_TEST_RELEASE
STATICCHECK_TEST_GO_VERSION="$(jq -r .go_version "$source_dir/profile.json")"
profile="$(sha256sum "$source_dir/SHA256SUMS" | cut -d ' ' -f1)"
STATICCHECK_TEST_IDENTITY="$(jq -r '.staticcheck_version + " " + .tools_version' "$source_dir/profile.json") $profile $STATICCHECK_TEST_GO_VERSION"
STATICCHECK_TEST_RELEASE="$(jq -r '"staticcheck " + .staticcheck_release + " (" + (.staticcheck_version | ltrimstr("v")) + ")"' "$source_dir/profile.json")"
cat >"$work/bin/go" <<'GO'
#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >>"$STATICCHECK_TEST_GO_LOG"
case "$*" in
  'env GOHOSTOS') echo linux ;;
  'env GOHOSTARCH') echo amd64 ;;
  'env GOVERSION') echo "$STATICCHECK_TEST_GO_VERSION" ;;
  *) echo 'fixture forbids builds and downloads' >&2; exit 99 ;;
esac
GO
chmod +x "$work/bin/go"
cache="$GOBIN/staticcheck-facts/$profile-linux-amd64"
mkdir -p "$cache"
cat >"$cache/staticcheck" <<'BINARY'
#!/usr/bin/env bash
set -euo pipefail
echo "$*" >>"$STATICCHECK_TEST_EXEC_LOG"
case "$*" in
  -stack-profile-version) printf '%s\n' "$STATICCHECK_TEST_IDENTITY" ;;
  -version) printf '%s\n' "$STATICCHECK_TEST_RELEASE" ;;
  *) exit 98 ;;
esac
BINARY
chmod +x "$cache/staticcheck"
(cd "$cache" && sha256sum staticcheck >BINARY.sha256)
[[ "$(bash "$work/source/build.sh" 2>/dev/null)" == "$cache/staticcheck" ]]
expect_failure() {
  local label="$1"
  shift
  if "$@" >"$work/failure.log" 2>&1; then
    echo "FAIL: $label accepted" >&2; exit 1
  fi
  echo "PASS: $label rejected"
}
expect_failure 'release pin mismatch' env STATICCHECK_VERSION=2026.1.0 bash "$work/source/build.sh"
expect_failure 'cached profile identity mismatch' env STATICCHECK_TEST_IDENTITY=wrong bash "$work/source/build.sh"
expect_failure 'cached release marker mismatch' env STATICCHECK_TEST_RELEASE='staticcheck 2026.2.1 (0.8.0)' bash "$work/source/build.sh"
executions="$(wc -l <"$STATICCHECK_TEST_EXEC_LOG")"
printf '\n' >>"$cache/staticcheck"
expect_failure 'cached binary corruption' bash "$work/source/build.sh"
[[ "$(wc -l <"$STATICCHECK_TEST_EXEC_LOG")" == "$executions" ]]
printf '\n' >>"$work/source/objectpath.patch"
expect_failure 'source input corruption' bash "$work/source/build.sh"
if grep -Evq '^env (GOHOSTOS|GOHOSTARCH|GOVERSION)$' "$STATICCHECK_TEST_GO_LOG"; then
  echo 'invalid cached artifacts must not trigger a replacement build' >&2; exit 1
fi
echo 'staticcheck facts build identity and fail-closed cache contracts passed'
