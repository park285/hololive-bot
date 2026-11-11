INSERT INTO schedule_posters (id, channel_id, x_user_id, post_url, image_url, posted_at, text_excerpt)
SELECT c.id, c.channel_id, c.x_user_id, c.post_url, c.image_url, c.posted_at, c.text_excerpt
FROM unnest($1::bigint[], $2::text[], $3::text[], $4::text[], $5::text[], $6::timestamptz[], $7::text[])
    AS c(id, channel_id, x_user_id, post_url, image_url, posted_at, text_excerpt)
ON CONFLICT (id) DO NOTHING
