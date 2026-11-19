SELECT o.video_id, o.title, o.scheduled_at, o.collabo_talent_names, COALESCE(h.channel_id, '')
FROM (
    SELECT DISTINCT ON (video_id) video_id, title, scheduled_at, collabo_talent_names
    FROM youtube_schedule_items
    WHERE provider = 'hololive_official' AND video_id = ANY($1)
    ORDER BY video_id, updated_at DESC
) o
LEFT JOIN LATERAL (
    SELECT channel_id
    FROM youtube_schedule_items
    WHERE video_id = o.video_id AND provider <> 'hololive_official' AND channel_id <> ''
    ORDER BY updated_at DESC
    LIMIT 1
) h ON true
WHERE cardinality(o.collabo_talent_names) > 0
