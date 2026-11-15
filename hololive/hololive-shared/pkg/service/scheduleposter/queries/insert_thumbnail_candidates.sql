-- 썸네일 후보 id는 'youtube_thumbnail:<영상 ID>:<그림 sha256>'의 sha256 앞 60비트 + 1이다. 같은 영상도 그림이 바뀌면 새 후보가 되고,
-- 같은 그림은 같은 id라 ON CONFLICT가 중복을 막는다. 생방송 기록이 없는 영상도 후보로 넣고 발췌만 비운다.
-- image_sha256은 같은 문장에서 저장하는 그림 바이트로 구하므로 저장한 그림과 어긋나지 않는다.
-- 그림 없이 들어온 같은 썸네일의 검토 대기·확정 행은 새 후보를 넣지 않는 주에도 이번에 받은 그림으로 채운다.
-- id와 해시가 같으면 같은 그림이라, 썸네일이 바뀌기 전에 받아 두면 그 뒤의 재검토·다시 뽑기가 수집 때 그림을 쓴다.
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
