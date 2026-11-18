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
