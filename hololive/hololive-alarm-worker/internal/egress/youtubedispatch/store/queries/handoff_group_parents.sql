-- handoff 실패 정산이 논리 그룹 delivery를 잠그기 전에 그 부모 outbox id를 잠그지 않고 읽는다. 후보 조건과 인자는
-- transition_group_rows.sql과 같은 조각·같은 값이고, 같은 (created_at, id) 순서의 $5행 안에서만 부모를 모으므로 그룹
-- 잠금이 볼 범위와 같다. 호출자는 이 id를 delivery_repository_aggregate_lock.sql로 먼저 잠근 뒤 그룹을 잠그고, 잠근
-- 그룹 행의 부모가 모두 이 집합에 있는지 확인한다.
WITH /* transition_group_candidates */
SELECT DISTINCT grouped.outbox_id
FROM (
    SELECT delivery.outbox_id
    FROM youtube_notification_delivery AS delivery
    JOIN candidate_ids ON candidate_ids.id = delivery.id
    ORDER BY delivery.created_at, delivery.id
    LIMIT $5
) AS grouped
ORDER BY grouped.outbox_id
