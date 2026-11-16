INSERT INTO schedule_poster_rows (poster_id, ordinal, day, start_time, end_time, tz, source_zone, status, source_text, badges,
                                  title_ko, with_ko, place_ko, tags_ko, note_ko, terms, doubt_ko, warnings)
SELECT $1, r.ordinal, r.day, NULLIF(r.start_time, ''), NULLIF(r.end_time, ''), r.tz, r.source_zone, r.status, r.source_text, r.badges,
       r.title_ko, r.with_ko, r.place_ko, r.tags_ko, r.note_ko, r.terms, r.doubt_ko, r.warnings
FROM jsonb_to_recordset($2::jsonb) AS r(
    ordinal smallint, day date, start_time text, end_time text, tz text, source_zone text, status text, source_text text, badges text[],
    title_ko text, with_ko text[], place_ko text, tags_ko text[], note_ko text, terms jsonb, doubt_ko text, warnings text[]
)
