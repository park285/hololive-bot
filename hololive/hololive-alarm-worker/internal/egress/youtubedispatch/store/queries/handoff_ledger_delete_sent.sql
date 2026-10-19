-- handoff 실패 정산이 같은 트랜잭션에서 그 요청 발송이 남긴 SENT 원장만 지운다. 논리 키($1·$2·$3)마다
-- source_delivery_id가 그 키의 owner($4)인 SENT 행만 대상이고, 다른 출처·QUARANTINED 원장은 건드리지 않는다. 호출자는
-- 같은 트랜잭션에서 원장 행을 이미 잠갔다(transition_ledger_rows.sql).
DELETE FROM youtube_notification_delivery_ledger AS ledger
USING unnest($1::TEXT[], $2::TEXT[], $3::TEXT[], $4::BIGINT[]) AS target(kind, logical_id, room_id, source_delivery_id)
WHERE ledger.kind = target.kind
  AND ledger.logical_id = target.logical_id
  AND ledger.room_id = target.room_id
  AND ledger.status = 'SENT'
  AND ledger.source_delivery_id = target.source_delivery_id
