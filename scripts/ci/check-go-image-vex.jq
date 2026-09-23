# 같은 바이너리의 package scan이 정확한 advisory와 module PURL의 코드 부재를 증명해야 한다.
# govulncheck는 module 경로의 '/'를 PathEscape하고 Trivy는 그대로 표기한다.
def purl_identity: gsub("%2[fF]"; "/");
. as $vex
| [$scan[0].Results[] | select(.Type == "gobinary" and .Target == $target)] as $results
| ($results | length) == 1
  and ($results[0].Vulnerabilities | type == "array" and length > 0)
  and ($vex.statements | type == "array" and length > 0)
  and all($vex.statements[];
    .status == "not_affected" and .justification == "vulnerable_code_not_present")
  and all($results[0].Vulnerabilities[];
    . as $finding
    | ($finding.PkgIdentifier.PURL | type == "string" and startswith("pkg:golang/"))
      and ([$vex.statements[]
        | select(.vulnerability.name == $finding.VulnerabilityID
          or ((.vulnerability.aliases // []) | index($finding.VulnerabilityID)) != null)
        | select(any(.products[]?.subcomponents[]?;
          (.["@id"] | purl_identity) == ($finding.PkgIdentifier.PURL | purl_identity)))
      ] | length > 0))
