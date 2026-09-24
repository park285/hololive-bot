#!/usr/bin/env bash
set -euo pipefail
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd -P)"
fixture="$(mktemp -d)"
trap 'rm -rf "$fixture"' EXIT
mkdir -p "$fixture/repo/scripts/ci" "$fixture/bin" "$fixture/reports"
cp "$root/scripts/ci/"{run-final-image-scan.sh,check-go-image-vex.jq,go-tooling.sh} "$fixture/repo/scripts/ci/"
git -C "$fixture/repo" init -q
printf 'local|linux/arm64|fixture:prod\n' >"$fixture/repo/scripts/ci/final-image-scan-manifest.txt"
cat >"$fixture/bin/trivy" <<'SH'
#!/usr/bin/env bash
set -eu
if [[ "$1" == --version ]]; then echo 'Version: 0.74.0'; exit; fi
[[ "$SCAN_CASE" != trivy-error ]] || exit 2
while [[ "$1" != --output ]]; do shift; done
out="$2"
if [[ "$SCAN_CASE" == malformed ]]; then printf '{' >"$out"; exit 1; fi
if [[ "$SCAN_CASE" == empty-report ]]; then echo '{"SchemaVersion":2,"Results":[]}' >"$out"; exit 0; fi
if [[ "$SCAN_CASE" == clean ]]; then echo '{"SchemaVersion":2,"Results":[{"Target":"fixture","Type":"debian"}]}' >"$out"; exit 0; fi
if [[ "$SCAN_CASE" == grpc-* ]]; then
  id=CVE-2026-84445
  purl=pkg:golang/google.golang.org/grpc@v1.84.0
  [[ "$SCAN_CASE" != grpc-wrong-version ]] || purl=pkg:golang/google.golang.org/grpc@v1.84.1
  jq -n --arg id "$id" --arg purl "$purl" \
    '{SchemaVersion:2,Results:[{Target:"app/bin/fixture",Type:"gobinary",Vulnerabilities:[{VulnerabilityID:$id,PkgIdentifier:{PURL:$purl}}]}]}' >"$out"
  exit 1
fi
kind=gobinary
[[ "$SCAN_CASE" != non-go ]] || kind=debian
jq -n --arg kind "$kind" '{SchemaVersion:2,Results:[{Target:"app/bin/fixture",Type:$kind,Vulnerabilities:[{VulnerabilityID:"GO-TEST-1",PkgIdentifier:{PURL:"pkg:golang/example.test/lib@v1.0.0"}}]}]}' >"$out"
exit 1
SH
cat >"$fixture/bin/docker" <<'SH'
#!/usr/bin/env bash
set -eu
case "$1" in
  image)
    if [[ "$*" == *Architecture* ]]; then echo linux/arm64; else echo sha256:fixture; fi ;;
  create) echo fixture-container ;;
  cp)
    [[ "$SCAN_CASE" != missing-binary ]] || exit 1
    printf 'fixture binary\n' >"$3" ;;
  rm) echo removed >>"$FIXTURE_CLEANUP" ;;
  *) exit 2 ;;
esac
SH
cat >"$fixture/bin/govulncheck" <<'SH'
#!/usr/bin/env bash
set -eu
if [[ "$1" == -version ]]; then echo 'govulncheck@v1.8.0'; exit; fi
if [[ "$1" == -mode=extract ]]; then
  [[ "$SCAN_CASE" != extract-error ]] || exit 2
  echo '{"name":"govulncheck-extract","version":"0.1.0"}'
  symbols='[{"pkg":"main","name":"main"}]'
  [[ "$SCAN_CASE" != stripped ]] || symbols='[]'
  modules='[]'
  if [[ "$SCAN_CASE" == grpc-* ]]; then
    modules='[{"Path":"google.golang.org/grpc","Version":"v1.84.0","Replace":null}]'
    symbols='[{"pkg":"google.golang.org/grpc/internal/transport","name":"http2Server.HandleStreams"}]'
  fi
  if [[ "$SCAN_CASE" == grpc-xds ]]; then
    symbols='[{"pkg":"google.golang.org/grpc/internal/transport","name":"http2Server.HandleStreams"},{"pkg":"google.golang.org/grpc/internal/xds/server","name":"RouteAndProcess"}]'
  fi
  if [[ "$SCAN_CASE" == grpc-no-transport ]]; then
    symbols='[{"pkg":"main","name":"main"}]'
  fi
  arch=arm64
  [[ "$SCAN_CASE" != wrong-arch ]] || arch=amd64
  jq -n --argjson symbols "$symbols" --argjson modules "$modules" --arg arch "$arch" \
    '{goos:"linux",goarch:$arch,pkgSymbols:$symbols,modules:$modules}'
  exit
fi
[[ "$*" == *-scan=package* && "$*" == *-format=openvex* ]] || exit 2
[[ "$SCAN_CASE" != analysis-error ]] || exit 2
status=not_affected
reason=vulnerable_code_not_present
id=GO-TEST-1
purl=pkg:golang/example.test/lib@v1.0.0
aliases='[]'
if [[ "$SCAN_CASE" == grpc-* ]]; then
  status=affected
  reason=''
  id=GO-2026-6443
  purl=pkg:golang/google.golang.org%2Fgrpc@v1.84.0
  aliases='["CVE-2026-84445"]'
fi
case "$SCAN_CASE" in
  encoded-absence) purl=pkg:golang/example.test%2Flib@v1.0.0 ;;
  affected) status=affected ;;
  unreachable) reason=vulnerable_code_not_in_execute_path ;;
  unknown-id) id=GO-TEST-2 ;;
  wrong-module) purl=pkg:golang/example.test/other@v1.0.0 ;;
  wrong-version) purl=pkg:golang/example.test/lib@v2.0.0 ;;
  missing-statements) echo '{}'; exit ;;
  grpc-wrong-id) id=GO-TEST-OTHER ;;
esac
jq -n --arg status "$status" --arg reason "$reason" --arg id "$id" --arg purl "$purl" --argjson aliases "$aliases" \
  '{statements:[{status:$status,justification:$reason,vulnerability:{name:$id,aliases:$aliases},products:[{subcomponents:[{"@id":$purl}]}]}]}'
SH
cat >"$fixture/bin/go" <<'SH'
#!/usr/bin/env bash
set -eu
[[ "${1:-}" == version && "${2:-}" == -m ]] || exit 2
[[ "$SCAN_CASE" == grpc-* ]] || exit 2
sum=h1:soMyaPJ8pAak5PIQ0DGBUir0XRo2fRoMqhNWMLlLxO0=
[[ "$SCAN_CASE" != grpc-wrong-sum ]] || sum=h1:wrong
printf '\tdep\tgoogle.golang.org/grpc\tv1.84.0\t%s\n' "$sum"
SH
chmod +x "$fixture/bin/"*
export PATH="$fixture/bin:$PATH" TMPDIR="$fixture/reports" FIXTURE_CLEANUP="$fixture/cleanup"
cd "$fixture/repo"
for scenario in clean absent encoded-absence grpc-fixed grpc-xds grpc-no-transport grpc-wrong-sum grpc-wrong-id grpc-wrong-version affected unreachable unknown-id wrong-module wrong-version missing-statements stripped wrong-arch extract-error analysis-error missing-binary trivy-error malformed empty-report non-go; do
  status=0
  SCAN_CASE="$scenario" bash scripts/ci/run-final-image-scan.sh >"$fixture/$scenario.log" 2>&1 || status=$?
  if [[ "$scenario" == clean ]]; then
    [[ "$status" == 0 ]] || { cat "$fixture/$scenario.log"; exit 1; }
  elif [[ "$scenario" == absent || "$scenario" == encoded-absence || "$scenario" == grpc-fixed ]]; then
    [[ "$status" == 0 ]] || { cat "$fixture/$scenario.log"; exit 1; }
    report_dir="$(awk '/^final image scan evidence:/ {print $NF}' "$fixture/$scenario.log")"
    jq -e '.Results[0].Vulnerabilities | length == 1' "$report_dir/1.trivy.json" >/dev/null
    [[ -s "$report_dir/1.1.extract.json" && -s "$report_dir/1.1.vex.json" && -s "$report_dir/1.1.sha256" ]]
  else
    [[ "$status" != 0 ]] || { echo "unexpected pass: $scenario" >&2; exit 1; }
  fi
  echo "PASS: $scenario"
done
[[ -s "$FIXTURE_CLEANUP" ]]
