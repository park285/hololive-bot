WITH requested AS (
    SELECT kind, logical_id, room_id
    FROM unnest($1::text[], $2::text[], $3::text[]) AS keys(kind, logical_id, room_id)
)
SELECT ledger.kind,
       ledger.logical_id,
       ledger.room_id,
       ledger.status,
       ledger.first_recorded_at,
       ledger.updated_at,
       ledger.sent_at,
       ledger.quarantined_at,
       ledger.source_delivery_id
FROM youtube_notification_delivery_ledger AS ledger
JOIN requested
  ON requested.kind = ledger.kind
 AND requested.logical_id = ledger.logical_id
 AND requested.room_id = ledger.room_id
ORDER BY ledger.kind, ledger.logical_id, ledger.room_id
FOR UPDATE OF ledger;
