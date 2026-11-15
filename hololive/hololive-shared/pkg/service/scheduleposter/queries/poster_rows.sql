SELECT ordinal, day, COALESCE(start_time, ''), COALESCE(end_time, ''), tz, source_zone, status,
       source_text, badges, title_ko, with_ko, place_ko, tags_ko, note_ko, doubt_ko, warnings
FROM schedule_poster_rows
WHERE poster_id = $1
ORDER BY ordinal
