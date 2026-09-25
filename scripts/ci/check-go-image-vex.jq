# Go 이미지 finding은 정확한 패키지 부재 증명만 허용합니다.
# 취약 버전의 호출 경로 부재나 릴리스별 예외로 affected 판정을 통과시키지 않습니다.
def purl_identity: gsub("%2[fF]"; "/");
def same_finding($finding; $statement):
  ($statement.vulnerability.name == $finding.VulnerabilityID
    or (($statement.vulnerability.aliases // []) | index($finding.VulnerabilityID)) != null)
  and any($statement.products[]?.subcomponents[]?;
    (."@id" | purl_identity) == ($finding.PkgIdentifier.PURL | purl_identity));
def accepted($finding; $statement):
  same_finding($finding; $statement)
  and $statement.status == "not_affected"
  and $statement.justification == "vulnerable_code_not_present";
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
