UPDATE youtube_live_sessions AS session
SET channel_id = $2,
    status = $3,
    title = COALESCE($4::text, session.title),
    topic_id = $5,
    thumbnail_url = $6,
    scheduled_start_time = $7,
    started_at = $8,
    ended_at = $9,
    live_first_seen_at = $10,
    last_seen_at = $11,
    is_premiere = $12,
    lifecycle_origin = $13,
    status_observed_at = $14,
    schedule_observed_at = $15,
    title_observed_at = $16
WHERE session.video_id = $1
  AND (session.channel_id, session.status, COALESCE(session.title, ''), session.topic_id, session.thumbnail_url,
       session.scheduled_start_time, session.started_at, session.ended_at, session.live_first_seen_at,
       session.last_seen_at, session.is_premiere, session.lifecycle_origin,
       session.status_observed_at, session.schedule_observed_at, session.title_observed_at)
      IS NOT DISTINCT FROM
      ($17::text, $18::text, $19::text, $20::text, $21::text,
       $22::timestamptz, $23::timestamptz, $24::timestamptz, $25::timestamptz,
       $26::timestamptz, $27::boolean, $28::text,
       $29::timestamptz, $30::timestamptz, $31::timestamptz)
