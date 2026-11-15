-- published_poster.sql이 고른 확정본의 행 묶음을 읽는다.
WITH poster AS (
    SELECT id, week_start, week_end, rows_extracted_at, posted_at, rows_status, rows_day_shift
    FROM schedule_posters
    WHERE id = (
/* published_poster */
    )
)
SELECT poster.id, poster.week_start, poster.week_end, poster.rows_extracted_at, poster.posted_at, poster.rows_status = 'pending',
       poster.rows_day_shift, r.ordinal, r.day, COALESCE(r.start_time, ''), COALESCE(r.end_time, ''), r.tz, r.source_zone, r.status,
       r.source_text, r.badges, r.title_ko, r.with_ko, r.place_ko, r.tags_ko, r.note_ko
FROM poster
JOIN schedule_poster_rows r ON r.poster_id = poster.id
ORDER BY r.ordinal
