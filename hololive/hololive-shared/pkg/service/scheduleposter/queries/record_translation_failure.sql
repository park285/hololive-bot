-- 새 판독이 없으면($4가 NULL) 이전 시도가 남긴 판독·번역표를 그대로 둔다.
UPDATE schedule_posters
SET translation_status = 'failed', translation_error = $3, revision = revision + 1, updated_at = now(),
    translation_inputs_sha256 = COALESCE($4::bytea, translation_inputs_sha256),
    source_transcript = CASE WHEN $4::bytea IS NULL THEN source_transcript ELSE $5::text[] END,
    translation_table = CASE WHEN $4::bytea IS NULL THEN translation_table ELSE NULLIF($6::text, '') END
WHERE id = $1 AND revision = $2 AND translation_status = 'pending' AND review_status = 'confirmed'
