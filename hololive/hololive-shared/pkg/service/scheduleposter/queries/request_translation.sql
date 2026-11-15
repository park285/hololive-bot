-- 재번역 예약은 일정표 행의 재추출도 함께 예약한다. 이전 번역본과 이전 행 묶음은 새 결과가 저장될 때까지 남는다.
WITH target AS (
    SELECT revision FROM schedule_posters WHERE id = $1
), updated AS (
    UPDATE schedule_posters
    SET translation_status = 'pending', translation_error = '', rows_status = 'pending', rows_error = '',
        rows_revision = rows_revision + 1, revision = revision + 1, updated_at = now()
    WHERE id = $1 AND revision = $2 AND review_status = 'confirmed' AND translation_status IN ('succeeded', 'failed')
    RETURNING revision
)
SELECT (SELECT revision FROM target), (SELECT revision FROM updated)
