-- 공식 일정이 콜라보 출연자를 적은 방송만 고른다. 공식 일정 행은 채널을 담지 않아, 여는 채널은 같은 영상의 다른 출처 행에서 읽고 없으면 빈 값이다.
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
