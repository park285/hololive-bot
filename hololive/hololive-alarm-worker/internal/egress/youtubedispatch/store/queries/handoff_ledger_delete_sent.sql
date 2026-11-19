DELETE FROM youtube_notification_delivery_ledger AS ledger
USING unnest($1::TEXT[], $2::TEXT[], $3::TEXT[], $4::BIGINT[]) AS target(kind, logical_id, room_id, source_delivery_id)
WHERE ledger.kind = target.kind
  AND ledger.logical_id = target.logical_id
  AND ledger.room_id = target.room_id
  AND ledger.status = 'SENT'
  AND ledger.source_delivery_id = target.source_delivery_id
