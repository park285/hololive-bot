-- published_image.sql과 같은 기준으로 확정본 하나를 고른다. 재추출 중이거나 재추출이 실패해도 남아 있는 이전 묶음을 보여 준다.
-- 오늘을 덮는지는 주 범위를 행 묶음의 하루 밀기 값(rows_day_shift)만큼 민 범위로 본다. JST로 바꾼 행이 주 범위보다 하루 늦거나 이른 포스터를 그 날짜에 보여 주기 위해서다.
-- 정렬은 밀지 않은 주 시작일로 해야 같은 주의 늦게 올린 수정본이 하루 밀기 값이 다른 이전 확정본보다 앞선다.
WITH poster AS (
    SELECT id, week_start, week_end, rows_extracted_at, posted_at, rows_status, rows_day_shift
    FROM schedule_posters
    WHERE channel_id = $1 AND review_status = 'confirmed' AND rows_extracted_at IS NOT NULL
      AND week_start + rows_day_shift <= $2::date AND week_end + rows_day_shift >= $2::date
    ORDER BY week_start DESC, posted_at DESC, id DESC
    LIMIT 1
)
SELECT poster.id, poster.week_start, poster.week_end, poster.rows_extracted_at, poster.posted_at, poster.rows_status = 'pending',
       poster.rows_day_shift, r.ordinal, r.day, COALESCE(r.start_time, ''), COALESCE(r.end_time, ''), r.tz, r.source_zone, r.status,
       r.source_text, r.badges, r.title_ko, r.with_ko, r.place_ko, r.tags_ko, r.note_ko
FROM poster
JOIN schedule_poster_rows r ON r.poster_id = poster.id
ORDER BY r.ordinal
