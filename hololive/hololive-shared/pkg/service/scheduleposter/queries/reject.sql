-- 일정표 행 묶음은 호출한 쪽이 같은 트랜잭션에서 delete_rows.sql로 지운다. 이 문장의 snapshot은 커밋 전이던 RecordRows의 행을 보지 못한다.
WITH target AS (
    SELECT revision FROM schedule_posters WHERE id = $1
), updated AS (
    UPDATE schedule_posters
    SET review_status = 'rejected', reviewed_at = now(), reviewed_by = $3, translation_status = 'not_requested', translation_error = '',
        translated_image = NULL, translated_mime = NULL, translated_at = NULL,
        rows_status = 'pending', rows_error = '', rows_extracted_at = NULL, rows_day_shift = 0, rows_revision = rows_revision + 1, revision = revision + 1, updated_at = now()
    WHERE id = $1 AND revision = $2 AND review_status IN ('pending_review', 'confirmed')
    RETURNING revision
)
SELECT (SELECT revision FROM target), (SELECT revision FROM updated)
