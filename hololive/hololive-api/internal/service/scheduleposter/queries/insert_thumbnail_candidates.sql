WITH candidate AS (
    SELECT ('x' || left(encode(sha256(convert_to('youtube_thumbnail:' || c.video_id || ':' || h.image_sha256, 'UTF8')), 'hex'), 15))::bit(60)::bigint + 1 AS id,
           c.channel_id, c.video_id, c.image_data, c.image_mime, c.posted_at, h.image_sha256
    FROM unnest($1::text[], $2::text[], $3::bytea[], $4::text[], $5::timestamptz[]) AS c(channel_id, video_id, image_data, image_mime, posted_at)
    CROSS JOIN LATERAL (SELECT encode(sha256(c.image_data), 'hex') AS image_sha256) AS h
), filled AS (
    UPDATE schedule_posters AS p
    SET image_data = candidate.image_data, image_mime = candidate.image_mime
    FROM candidate
    WHERE p.id = candidate.id AND p.source = 'youtube_thumbnail' AND p.image_sha256 = candidate.image_sha256
      AND p.image_data IS NULL AND p.review_status IN ('pending_review', 'confirmed')
    RETURNING p.id
)
INSERT INTO schedule_posters (
    id, source, channel_id, x_user_id, post_url, image_url, image_sha256, image_data, image_mime, posted_at, text_excerpt, review_requested_at
)
SELECT c.id, 'youtube_thumbnail', c.channel_id, '', 'https://www.youtube.com/watch?v=' || c.video_id,
       'https://i.ytimg.com/vi/' || c.video_id || '/maxresdefault.jpg', c.image_sha256, c.image_data, c.image_mime, c.posted_at,
       COALESCE((SELECT left(s.title, 500) FROM youtube_live_sessions s WHERE s.video_id = c.video_id), ''),
       CASE WHEN $6::boolean THEN NULL ELSE now() END
FROM candidate AS c
WHERE NOT EXISTS (
    SELECT 1 FROM schedule_posters confirmed
    WHERE confirmed.channel_id = c.channel_id AND confirmed.review_status = 'confirmed'
      AND (c.posted_at AT TIME ZONE 'Asia/Tokyo')::date >= confirmed.week_start
      AND (c.posted_at AT TIME ZONE 'Asia/Tokyo')::date < confirmed.week_end
)
ON CONFLICT (id) DO NOTHING
