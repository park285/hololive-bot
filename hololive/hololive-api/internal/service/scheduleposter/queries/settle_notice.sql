UPDATE schedule_poster_notices n
SET status = $2, rooms = $3, settled_at = now(), lease_until = NULL
WHERE n.status = 'pending'
  AND (n.id = $1
       OR ($2 = 'sent' AND n.kind = 'changed' AND n.channel_id = $4 AND n.poster_id = $5 AND n.created_at <= $6
           AND (n.lease_until IS NULL OR n.lease_until < now())
           AND /* fragment:current_change */

           ))
