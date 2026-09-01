SELECT ledger.kind, ledger.logical_id, ledger.room_id, ledger.status
FROM youtube_notification_delivery_ledger AS ledger
JOIN unnest($1::text[], $2::text[], $3::text[]) AS input(kind, logical_id, room_id)
  ON input.kind = ledger.kind
 AND input.logical_id = ledger.logical_id
 AND input.room_id = ledger.room_id;
