(.target | type == "object" and length > 0)
and all(.target[];
  (if has("attest") then .attest else [] end) as $attest
  | ($attest | type == "array")
    and all($attest[];
      . == "type=provenance,disabled=true" or . == "type=sbom,disabled=true"
    )
)
