# 소유: Hololive release/security. GO-2026-6443의 범위에는 grpc-go v1.84.0이
# 들어가지만 공식 v1.84.0 릴리스 노트는 :authority/Host 거부 수정을 명시합니다.
# https://github.com/grpc/grpc-go/releases/tag/v1.84.0
# https://github.com/grpc/grpc-go/security/advisories/GHSA-2v4p-qf9q-27wj
# 정확한 모듈 checksum과 transport 심볼을 확인하고 xDS 라우팅 심볼이 없을 때만
# 허용합니다. 원시 결과는 보존하며 다른 탐지는 계속 차단합니다. 2026-10-31 또는
# 다음 안정 grpc-go 릴리스/Go 취약점 DB 정정 시 재검토하고 제거합니다.
def purl_identity: gsub("%2[fF]"; "/");
def same_finding($finding; $statement):
  ($statement.vulnerability.name == $finding.VulnerabilityID
    or (($statement.vulnerability.aliases // []) | index($finding.VulnerabilityID)) != null)
  and any($statement.products[]?.subcomponents[]?;
    (."@id" | purl_identity) == ($finding.PkgIdentifier.PURL | purl_identity));
def fixed_grpc_184($finding; $statement):
  $finding.VulnerabilityID == "CVE-2026-84445"
  and $finding.PkgIdentifier.PURL == "pkg:golang/google.golang.org/grpc@v1.84.0"
  and $statement.vulnerability.name == "GO-2026-6443"
  and $statement.status == "affected"
  and $grpc_release == "v1.84.0 h1:soMyaPJ8pAak5PIQ0DGBUir0XRo2fRoMqhNWMLlLxO0="
  and ($extract | length) == 2
  and $extract[0].name == "govulncheck-extract"
  and $extract[1].goos == "linux" and $extract[1].goarch == "arm64"
  and ($extract[1].modules | type) == "array"
  and ([ $extract[1].modules[]
    | select(.Path == "google.golang.org/grpc" and .Version == "v1.84.0" and .Replace == null) ] | length) == 1
  and ($extract[1].pkgSymbols | type) == "array"
  and ([ $extract[1].pkgSymbols[]
    | select(.pkg == "google.golang.org/grpc/internal/transport" and .name == "http2Server.HandleStreams") ] | length) == 1
  and ([ $extract[1].pkgSymbols[]
    | select(.pkg == "google.golang.org/grpc/internal/xds/server" and (.name | contains("RouteAndProcess"))) ] | length) == 0;
def accepted($finding; $statement):
  same_finding($finding; $statement)
  and ((($statement.status == "not_affected" and $statement.justification == "vulnerable_code_not_present")
    or fixed_grpc_184($finding; $statement)));
. as $vex
| [ $scan[0].Results[] | select(.Type == "gobinary" and .Target == $target) ] as $results
| ($results | length) == 1
  and ($results[0].Vulnerabilities | type == "array" and length > 0)
  and ($vex.statements | type == "array" and length == ($results[0].Vulnerabilities | length))
  and all($results[0].Vulnerabilities[];
    . as $finding
    | ($finding.PkgIdentifier.PURL | type == "string" and startswith("pkg:golang/"))
      and any($vex.statements[]; accepted($finding; .)))
  and all($vex.statements[]; . as $statement
    | any($results[0].Vulnerabilities[]; accepted(.; $statement)))
