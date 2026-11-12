-- 커뮤니티 후보 id는 글 ID sha256의 앞 60비트 + 1이다. 2^60보다 작아 수집 범위(최근 8일)의 X 게시물 번호(약 2×10^18)와 겹치지 않고,
-- 같은 글은 같은 id가 되어 ON CONFLICT가 중복을 막는다. 저장된 이미지 URL은 640px로 잘린 썸네일이라 크기 지정을 =s0(원본)으로 바꾼다.
WITH posts AS (
    SELECT p.post_id, p.channel_id, p.content_text, COALESCE(p.published_at, p.first_seen_at) AS posted_at,
           regexp_replace(p.post_id, '^community:', '') AS public_id, p.images -> 0 ->> 'url' AS image_url
    FROM youtube_community_posts p
    WHERE p.channel_id = ANY($1::text[]) AND COALESCE(p.published_at, p.first_seen_at) >= $2
      AND p.content_text ~* $3 AND jsonb_typeof(p.images) = 'array'
)
INSERT INTO schedule_posters (id, source, channel_id, x_user_id, post_url, image_url, posted_at, text_excerpt)
SELECT ('x' || left(encode(sha256(convert_to(posts.post_id, 'UTF8')), 'hex'), 15))::bit(60)::bigint + 1,
       'youtube_community', posts.channel_id, '', 'https://www.youtube.com/post/' || posts.public_id,
       regexp_replace(posts.image_url, '=[^/]*$', '=s0'), posts.posted_at, left(posts.content_text, 500)
FROM posts
WHERE posts.public_id ~ '^[A-Za-z0-9_-]+$' AND posts.image_url LIKE 'https://yt3.ggpht.com/%'
  AND NOT EXISTS (
    SELECT 1 FROM schedule_posters confirmed
    WHERE confirmed.channel_id = posts.channel_id AND confirmed.review_status = 'confirmed'
      AND (posts.posted_at AT TIME ZONE 'Asia/Tokyo')::date >= confirmed.week_start
      AND (posts.posted_at AT TIME ZONE 'Asia/Tokyo')::date < confirmed.week_end
)
ON CONFLICT (id) DO NOTHING
