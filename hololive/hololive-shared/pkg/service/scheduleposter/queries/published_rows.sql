-- published_image.sql과 같은 기준으로 확정본 하나를 고른다. 재추출 중이거나 재추출이 실패해도 남아 있는 이전 묶음을 보여 준다.
WITH poster AS (
    SELECT id, week_start, week_end, rows_extracted_at, posted_at, rows_status
    FROM schedule_posters
    WHERE channel_id = $1 AND review_status = 'confirmed' AND rows_extracted_at IS NOT NULL
      AND week_start <= $2::date AND week_end >= $2::date
    ORDER BY week_start DESC, posted_at DESC, id DESC
    LIMIT 1
)
SELECT poster.id, poster.week_start, poster.week_end, poster.rows_extracted_at, poster.posted_at, poster.rows_status = 'pending',
       r.ordinal, r.day, COALESCE(r.start_time, ''), COALESCE(r.end_time, ''), r.tz, r.status, r.source_text, r.badges,
       r.title_ko, r.with_ko, r.place_ko, r.tags_ko, r.note_ko
FROM poster
JOIN schedule_poster_rows r ON r.poster_id = poster.id
ORDER BY r.ordinal
