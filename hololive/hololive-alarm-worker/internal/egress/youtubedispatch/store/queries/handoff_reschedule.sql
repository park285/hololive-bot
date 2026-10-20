-- 전이 없이 다음 확인을 clock + $9 밀리초로 예약하되 admission(멤버 sent_at) + 지평 $10 밀리초를 넘기지 않는다. 지평
-- 시각의 확인이 마지막 확인이다. 멤버 delivery는 바꾸지 않으므로 잠그지 않고 읽는다. claim 때 이 요청에 묶인 SENT
-- 멤버($7, 행 버전 $8)가 고정 멤버 전원이고 지금도 같은 행 버전의 SENT이면(members_match) 예약한다. 아니면 요청을
-- 다시 모을 수 없으므로 멤버를 바꾸지 않고 확인을 닫는다.
/* handoff_request_fence */, members AS MATERIALIZED (
    SELECT count(d.id) = cardinality($6::BIGINT[]) AS members_match,
           min(d.sent_at) AS admitted_at
    FROM unnest($7::BIGINT[], $8::BIGINT[]) AS claimed(id, row_version)
    JOIN youtube_notification_delivery d
      ON d.id = claimed.id
     AND d.row_version = claimed.row_version
     AND d.status = 'SENT'
     AND d.send_request_id = $1
), updated AS (
    UPDATE youtube_notification_send_request r
    SET handoff_next_check_at = CASE
            WHEN members.members_match THEN LEAST(
                clock.now + ($9::BIGINT * INTERVAL '1 millisecond'),
                members.admitted_at + ($10::BIGINT * INTERVAL '1 millisecond'))
        END,
        handoff_checked_at = CASE WHEN members.members_match THEN NULL ELSE clock.now END
    FROM checked
    CROSS JOIN clock
    CROSS JOIN members
    WHERE r.base_id = $1
      AND checked.token_matches
      AND checked.request_matches
    RETURNING r.base_id
)
SELECT checked.token_matches, checked.request_matches, members.members_match,
       EXISTS (SELECT 1 FROM updated) AS applied
FROM checked
CROSS JOIN members
