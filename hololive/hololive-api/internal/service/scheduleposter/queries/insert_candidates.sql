INSERT INTO schedule_posters (id, channel_id, x_user_id, post_url, image_url, posted_at, text_excerpt, review_requested_at)
SELECT c.id, c.channel_id, c.x_user_id, c.post_url, c.image_url, c.posted_at, c.text_excerpt, CASE WHEN $8::boolean THEN NULL ELSE now() END
FROM unnest($1::bigint[], $2::text[], $3::text[], $4::text[], $5::text[], $6::timestamptz[], $7::text[])
    AS c(id, channel_id, x_user_id, post_url, image_url, posted_at, text_excerpt)
WHERE NOT EXISTS (
    SELECT 1 FROM schedule_posters confirmed
    WHERE confirmed.channel_id = c.channel_id AND confirmed.review_status = 'confirmed'
      AND (c.posted_at AT TIME ZONE 'Asia/Tokyo')::date >= confirmed.week_start
      AND (c.posted_at AT TIME ZONE 'Asia/Tokyo')::date < confirmed.week_end
)
ON CONFLICT (id) DO NOTHING
