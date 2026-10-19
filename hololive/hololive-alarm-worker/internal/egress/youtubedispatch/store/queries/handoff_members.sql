-- handoff 실패 정산이 고정 멤버의 논리 identity를 얻으려고 멤버 delivery와 부모 outbox를 읽는다. identity 열은 행이
-- 생긴 뒤 바뀌지 않으므로 잠그지 않는다. 잠금은 뒤의 handoff_group_parents.sql로 읽은 부모 outbox에 먼저, 이어서
-- transition_group_rows.sql이 논리 그룹 전체에 같은 순서로 건다.
SELECT delivery.id,
       delivery.outbox_id,
       delivery.room_id,
       delivery.status,
       delivery.attempt_count,
       delivery.next_attempt_at,
       delivery.created_at,
       delivery.locked_at,
       delivery.locked_by,
       delivery.sent_at,
       COALESCE(delivery.error, '') AS error,
       delivery.row_version,
       outbox.kind,
       outbox.channel_id,
       outbox.content_id,
       outbox.payload::text AS payload,
       outbox.created_at AS outbox_created_at,
       outbox.sent_at AS outbox_sent_at
FROM youtube_notification_delivery AS delivery
JOIN youtube_notification_outbox AS outbox ON outbox.id = delivery.outbox_id
WHERE delivery.id = ANY ($1::BIGINT[])
ORDER BY delivery.id
