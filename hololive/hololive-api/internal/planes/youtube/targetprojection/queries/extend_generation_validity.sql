UPDATE youtube_collection_projection_generations
-- supplied now의 역순 호출만 막고 정상 TTL 하향은 즉시 반영한다.
-- migration 전 갱신 시각은 추정하지 않고 첫 성공 refresh에서 수립한다.
SET valid_until = CASE WHEN validity_refreshed_at IS NULL OR validity_refreshed_at <= $4
                       THEN $2 ELSE valid_until END,
    validity_refreshed_at = GREATEST(validity_refreshed_at, $4),
    eligibility_version = eligibility_version + CASE WHEN $3::boolean THEN 1 ELSE 0 END
WHERE generation = $1 AND status = 'CURRENT'
