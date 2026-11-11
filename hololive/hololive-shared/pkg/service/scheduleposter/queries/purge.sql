DELETE FROM schedule_posters
WHERE id IN (
    SELECT id FROM schedule_posters
    WHERE COALESCE(week_end, (created_at AT TIME ZONE 'Asia/Tokyo')::date) < $1::date
      AND review_status <> 'pending_review'
    ORDER BY id
    LIMIT $2
)
