-- 썸네일 후보 id는 'youtube_thumbnail:<영상 ID>:<그림 sha256>'의 sha256 앞 60비트 + 1이다. 같은 영상도 그림이 바뀌면 새 후보가 되고,
-- 같은 그림은 같은 id라 ON CONFLICT가 중복을 막는다. 생방송 기록이 없는 영상도 후보로 넣고 발췌만 비운다.
INSERT INTO schedule_posters (id, source, channel_id, x_user_id, post_url, image_url, image_sha256, posted_at, text_excerpt, review_requested_at)
SELECT ('x' || left(encode(sha256(convert_to('youtube_thumbnail:' || c.video_id || ':' || c.image_sha256, 'UTF8')), 'hex'), 15))::bit(60)::bigint + 1,
       'youtube_thumbnail', c.channel_id, '', 'https://www.youtube.com/watch?v=' || c.video_id,
       'https://i.ytimg.com/vi/' || c.video_id || '/maxresdefault.jpg', c.image_sha256, c.posted_at,
       COALESCE((SELECT left(s.title, 500) FROM youtube_live_sessions s WHERE s.video_id = c.video_id), ''),
       CASE WHEN $5::boolean THEN NULL ELSE now() END
FROM unnest($1::text[], $2::text[], $3::text[], $4::timestamptz[]) AS c(channel_id, video_id, image_sha256, posted_at)
WHERE NOT EXISTS (
    SELECT 1 FROM schedule_posters confirmed
    WHERE confirmed.channel_id = c.channel_id AND confirmed.review_status = 'confirmed'
      AND (c.posted_at AT TIME ZONE 'Asia/Tokyo')::date >= confirmed.week_start
      AND (c.posted_at AT TIME ZONE 'Asia/Tokyo')::date < confirmed.week_end
)
ON CONFLICT (id) DO NOTHING
