SELECT mode,
       generation
FROM source_authority_fences
WHERE source_kind = $1
FOR SHARE
