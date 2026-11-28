SELECT COALESCE(array_agg(n.post_id ORDER BY n.post_id), '{}'::bigint[])
FROM schedule_poster_notices claimed
JOIN schedule_poster_notices n ON n.kind = 'changed' AND n.channel_id = claimed.channel_id AND n.poster_id = claimed.poster_id
WHERE claimed.id = $1
  AND (n.id = claimed.id
       OR (n.status = 'pending' AND n.created_at <= $2 AND (n.lease_until IS NULL OR n.lease_until < now())))
  AND /* fragment:current_change */

