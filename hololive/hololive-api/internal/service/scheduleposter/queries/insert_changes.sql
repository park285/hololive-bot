INSERT INTO schedule_poster_changes (post_id, channel_id, item, kind, reason, slot, target_post_id, target_item, day, start_time,
                                     status, source_text, title_ko, with_ko, place_ko, tags_ko, note_ko, terms, doubt_ko, warnings)
SELECT $1, p.channel_id, c.item, c.kind, c.reason, NULLIF(c.slot, 0), NULLIF(c.target_post_id, 0), NULLIF(c.target_item, 0), c.day,
       c.start_time, NULLIF(c.status, ''), c.source_text, NULLIF(c.title_ko, ''), c.with_ko, c.place_ko, c.tags_ko, c.note_ko, c.terms,
       c.doubt_ko, c.warnings
FROM schedule_posters p
CROSS JOIN jsonb_to_recordset($3::jsonb) AS c(
    item smallint, kind text, reason text, slot smallint, target_post_id bigint, target_item smallint, day date, start_time text,
    status text, source_text text, title_ko text, with_ko text[], place_ko text, tags_ko text[], note_ko text, terms jsonb,
    doubt_ko text, warnings text[]
)
WHERE p.id = $2
