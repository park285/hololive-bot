-- 방마다 한 번에 하나, (next_attempt_at, created_at, id) 순서로 head-of-line을 지킨다. 같은 방에 SENDING 행, Iris
-- admission 뒤 handoff 확인이 끝나지 않은 SENT 행, 또는 더 앞선 due PENDING 행이 있으면 후행 행을 고르지 않는다.
-- 확인 대기 SENT 행은 handoff_next_check_at IS NOT NULL로 고른다. migration 330 CHECK가 다음 확인 시각을 정확히
-- SENT·handoff_request_id 있음·handoff_checked_at 없음인 행에만 두므로 같은 집합이고, 이 술어는 부분 index
-- idx_ndo_handoff_due의 조건과 같아 planner가 그 부분 index를 쓸 수 있다. 확인 대기 행은 handoff reconciler가 확인
-- 완료·재발급·종료 실패·격리로 끝내며, 다음 확인 시각은 admission + 지평(attempt_timeout)을 넘지 않는다.
o.status = 'PENDING'
AND o.next_attempt_at <= now_value.value
AND (o.locked_at IS NULL OR o.lock_expires_at <= now_value.value)
AND NOT EXISTS (
    SELECT 1 FROM notification_delivery_outbox earlier
    WHERE earlier.room_id = o.room_id
      AND (earlier.status = 'SENDING'
           OR earlier.handoff_next_check_at IS NOT NULL
           OR (earlier.status = 'PENDING'
               AND earlier.next_attempt_at <= now_value.value
               AND (earlier.next_attempt_at, earlier.created_at, earlier.id)
                   < (o.next_attempt_at, o.created_at, o.id)))
)
