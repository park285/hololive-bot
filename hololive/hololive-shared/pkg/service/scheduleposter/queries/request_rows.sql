-- 일정표 행만 다시 뽑는다. 민 주 범위가 끝난 확정본은 pending_rows.sql이 뽑지 않아 대기로 남으므로 받지 않는다.
-- revision, 번역 상태, 행 묶음, rows_extracted_at은 건드리지 않아 새 묶음이 저장될 때까지 이전 묶음이 게시된다.
WITH target AS (
    SELECT revision FROM schedule_posters WHERE id = $1
), updated AS (
    UPDATE schedule_posters
    SET rows_status = 'pending', rows_error = '', rows_revision = rows_revision + 1
    WHERE id = $1 AND revision = $2 AND review_status = 'confirmed' AND rows_status IN ('succeeded', 'failed')
      AND week_end + rows_day_shift >= (now() AT TIME ZONE 'Asia/Tokyo')::date
    RETURNING revision
)
SELECT (SELECT revision FROM target), (SELECT revision FROM updated)
