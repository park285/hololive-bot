WITH target AS (
    SELECT revision, review_status = 'pending_review' AS changeable,
           abs($3::date - (posted_at AT TIME ZONE 'Asia/Tokyo')::date) <= 7 AS near_post
    FROM schedule_posters
    WHERE id = $1
), updated AS (
    UPDATE schedule_posters
    SET review_status = 'confirmed', week_start = $3::date, week_end = $4::date, reviewed_at = now(), reviewed_by = $5,
        translation_status = 'pending', translation_error = '', revision = revision + 1, updated_at = now()
    WHERE id = $1 AND revision = $2 AND review_status = 'pending_review'
      AND abs($3::date - (posted_at AT TIME ZONE 'Asia/Tokyo')::date) <= 7
    RETURNING revision, channel_id
), superseded AS (
    UPDATE schedule_posters AS other
    SET review_status = 'expired', revision = other.revision + 1, updated_at = now()
    FROM updated
    WHERE other.channel_id = updated.channel_id AND other.id <> $1 AND other.review_status = 'pending_review'
      AND (other.posted_at AT TIME ZONE 'Asia/Tokyo')::date >= $3::date
      AND (other.posted_at AT TIME ZONE 'Asia/Tokyo')::date < $4::date
)
SELECT (SELECT revision FROM target), (SELECT changeable FROM target), (SELECT near_post FROM target),
       (SELECT revision FROM updated)
