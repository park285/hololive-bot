-- 이전 행 묶음과 rows_extracted_at은 남겨 계속 게시한다.
UPDATE schedule_posters
SET rows_status = 'failed', rows_error = $3
WHERE id = $1 AND rows_revision = $2 AND review_status = 'confirmed' AND rows_status = 'pending'
