-- handoff 적용 문장(handoff_*.sql)이 함께 쓰는 fence 앞부분이며 각 문장의 handoff_fence 표시 자리에 들어간다.
-- 단위($1)를 잠그고, 단위 id를 initplan으로 읽어 단위 잠금을 끝낸 뒤 멤버 전원을 id 순서로 잠근다. clock은 모든
-- 잠금 뒤 한 번 채취한다. 조건은 sendfence 관측 열이다.
--   status_matches: 멤버가 sent다.
--   version_matches: 단위의 soft claim 토큰($2 = handoff_next_check_at)이 그대로이고 확인이 끝나지 않았다.
--   request_matches: 단위의 client_request_id($3)·Iris requestId($4)가 claim 때와 같다.
--   members_non_terminal: 고정 멤버가 모두 남아 있고 sent가 아닌 멤버가 없다.
--   membership_matches: 현재 멤버가 고정 멤버 밖에 없다.
-- passed는 단위 멤버 전원의 조건이 참인지, held는 확인 소유(토큰·요청 일치)가 남았는지다. 멤버 조건만 어긋나
-- held이면서 passed가 아니면 단위를 다시 모을 수 없으므로 각 문장은 멤버를 바꾸지 않고 확인을 닫는다. 각 문장은
-- held를 check_held 열로 모든 행에 싣고 held에서 checked를 LEFT JOIN해, 잠근 멤버가 없어도 관측 열이 NULL인 행
-- 하나로 확인 소유를 돌려준다.
WITH unit AS MATERIALIZED (
    SELECT u.id, u.client_request_id, u.request_delivery_ids,
           u.handoff_request_id, u.handoff_next_check_at, u.handoff_checked_at
    FROM alarm_dispatch_send_units u
    WHERE u.id = $1
    FOR UPDATE
), locked AS MATERIALIZED (
    SELECT d.id, d.status, d.sent_at
    FROM alarm_dispatch_deliveries d
    WHERE d.send_unit_id = (SELECT unit.id FROM unit)
    ORDER BY d.id
    FOR UPDATE
), clock AS MATERIALIZED (
    SELECT clock_timestamp() AS now
    FROM (SELECT count(locked.id) AS locked_count FROM locked) AS acquired
), unit_checked AS MATERIALIZED (
    SELECT COALESCE(unit.handoff_next_check_at = $2::TIMESTAMPTZ AND unit.handoff_checked_at IS NULL, FALSE) AS token_matches,
           COALESCE(unit.client_request_id = $3 AND unit.handoff_request_id = $4, FALSE) AS request_matches,
           COALESCE(unit.request_delivery_ids @> members.ids, FALSE) AS membership_matches,
           COALESCE(unit.request_delivery_ids <@ members.ids, FALSE)
               AND NOT EXISTS (SELECT 1 FROM locked WHERE locked.status <> 'sent') AS members_sent
    FROM unit
    CROSS JOIN (SELECT COALESCE(array_agg(locked.id ORDER BY locked.id), '{}') AS ids FROM locked) AS members
), checked AS MATERIALIZED (
    SELECT locked.id, locked.status,
           locked.status = 'sent' AS status_matches,
           COALESCE(unit_checked.token_matches, FALSE) AS version_matches,
           COALESCE(unit_checked.membership_matches, FALSE) AS membership_matches,
           COALESCE(unit_checked.members_sent, FALSE) AS members_non_terminal,
           COALESCE(unit_checked.request_matches, FALSE) AS request_matches
    FROM locked
    LEFT JOIN unit_checked ON TRUE
), passed AS MATERIALIZED (
    SELECT EXISTS (SELECT 1 FROM checked) AND NOT EXISTS (
        SELECT 1 FROM checked
        WHERE NOT (checked.status_matches AND checked.version_matches AND checked.membership_matches
                   AND checked.members_non_terminal AND checked.request_matches)
    ) AS value
), held AS MATERIALIZED (
    SELECT EXISTS (
        SELECT 1 FROM unit_checked WHERE unit_checked.token_matches AND unit_checked.request_matches
    ) AS value
)
