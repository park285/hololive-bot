-- 확정본의 주 범위를 바꾸고 판정 주체를 관리자로 기록한다.
-- 일정표 행의 날짜는 주 범위로 정하므로 행 묶음은 지우고 다시 뽑는다. 틀린 주로 뽑은 날짜를 그 사이 게시하지 않기 위해서다.
-- 묶음은 호출한 쪽이 같은 트랜잭션에서 delete_rows.sql로 지운다. 이 문장의 snapshot은 커밋 전이던 RecordRows의 행을 보지 못한다.
-- review_requested_at은 summary의 최신 후보를 바꾸므로 건드리지 않으며, 그래서 같은 주 판정(week_coverage)은 자동 확정본으로 계속 본다.
WITH target AS (
    SELECT revision, review_status = 'confirmed' AS changeable,
           abs($3::date - (posted_at AT TIME ZONE 'Asia/Tokyo')::date) <= 7 AS near_post
    FROM schedule_posters
    WHERE id = $1
), updated AS (
    UPDATE schedule_posters
    SET week_start = $3::date, week_end = $4::date, reviewed_at = now(), reviewed_by = 'admin',
        rows_status = 'pending', rows_error = '', rows_extracted_at = NULL, rows_day_shift = 0, rows_revision = rows_revision + 1, revision = revision + 1, updated_at = now()
    WHERE id = $1 AND revision = $2 AND review_status = 'confirmed'
      AND abs($3::date - (posted_at AT TIME ZONE 'Asia/Tokyo')::date) <= 7
    RETURNING revision
)
SELECT (SELECT revision FROM target), (SELECT changeable FROM target), (SELECT near_post FROM target),
       (SELECT revision FROM updated)
