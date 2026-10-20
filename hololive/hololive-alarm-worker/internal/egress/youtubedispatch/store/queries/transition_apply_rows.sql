-- 행 fence는 (status, locked_by, row_version)이다. 상태·시도·잠금 시각을 바꾸는 모든 쓰기가 row_version을 올리므로
-- 버전 등가가 나머지 열의 등가를 함의한다. $15가 NULL이 아니면 발송 시작 전이로서 claim lease가 살아 있어야 한다.
-- 한 명령의 대상은 함께 전이하므로 전원이 fence를 통과할 때만 갱신하고, 잠근 대상마다 sendfence 관측 열과
-- 전이 시각(clock)을 돌려준다.
-- 시각 열은 대상을 잠근 뒤 한 번 채취한 clock으로만 정한다.
--   next_attempt_at: 지연($8)이 NULL이면 유지, 아니면 GREATEST(clock + 지연, 하한 $9). 하한은 DB 행에서 온 시각만
--                    넘기며, GREATEST는 NULL 하한을 무시한다.
--   locked_at:       $10이 참이면 clock, 아니면 NULL.
--   sent_at:         $12가 참이면 clock, 아니면 $13(DB 유래 ledger sent_at 또는 NULL).
WITH input AS (
    SELECT id,
           expected_status,
           expected_version,
           expected_locked_by,
           next_status,
           next_version,
           next_attempt,
           due_delay_ms,
           due_floor,
           lock_from_clock,
           next_locked_by,
           sent_from_clock,
           next_sent_at,
           next_error
    FROM unnest(
        $1::bigint[],
        $2::text[],
        $3::bigint[],
        $4::text[],
        $5::text[],
        $6::bigint[],
        $7::integer[],
        $8::bigint[],
        $9::timestamptz[],
        $10::boolean[],
        $11::text[],
        $12::boolean[],
        $13::timestamptz[],
        $14::text[]
    ) AS values(
        id,
        expected_status,
        expected_version,
        expected_locked_by,
        next_status,
        next_version,
        next_attempt,
        due_delay_ms,
        due_floor,
        lock_from_clock,
        next_locked_by,
        sent_from_clock,
        next_sent_at,
        next_error
    )
), locked AS MATERIALIZED (
    SELECT delivery.id, delivery.status, delivery.row_version, delivery.locked_by, delivery.locked_at
    FROM youtube_notification_delivery AS delivery
    WHERE delivery.id IN (SELECT id FROM input)
    ORDER BY delivery.id
    FOR UPDATE
), clock AS MATERIALIZED (
    SELECT clock_timestamp() AS at
    FROM (SELECT count(id) AS locked_count FROM locked) AS acquired
), checked AS MATERIALIZED (
    SELECT input.id,
           input.next_status,
           input.next_version,
           input.next_attempt,
           input.due_delay_ms,
           input.due_floor,
           input.lock_from_clock,
           input.next_locked_by,
           input.sent_from_clock,
           input.next_sent_at,
           input.next_error,
           locked.status AS current_status,
           locked.status = input.expected_status AS status_matches,
           locked.locked_by IS NOT DISTINCT FROM input.expected_locked_by AS owner_matches,
           locked.row_version = input.expected_version AS version_matches,
           ($15::bigint IS NULL OR (
               locked.locked_at IS NOT NULL
               AND locked.locked_at + $15::bigint * INTERVAL '1 millisecond' > (SELECT at FROM clock)
           )) AS lease_valid
    FROM input
    JOIN locked ON locked.id = input.id
), fenced AS (
    SELECT count(id) = cardinality($1::bigint[]) AS complete
    FROM checked
    WHERE status_matches AND owner_matches AND version_matches AND lease_valid
), updated AS (
    UPDATE youtube_notification_delivery AS delivery
    SET status = checked.next_status,
        row_version = checked.next_version,
        attempt_count = checked.next_attempt,
        next_attempt_at = CASE
            WHEN checked.due_delay_ms IS NULL THEN delivery.next_attempt_at
            ELSE GREATEST(
                (SELECT at FROM clock) + checked.due_delay_ms * INTERVAL '1 millisecond',
                checked.due_floor
            )
        END,
        locked_at = CASE WHEN checked.lock_from_clock THEN (SELECT at FROM clock) END,
        locked_by = checked.next_locked_by,
        sent_at = CASE WHEN checked.sent_from_clock THEN (SELECT at FROM clock) ELSE checked.next_sent_at END,
        error = checked.next_error
    FROM checked
    WHERE delivery.id = checked.id
      AND (SELECT complete FROM fenced)
    RETURNING delivery.id
)
SELECT checked.id,
       checked.current_status,
       checked.status_matches,
       checked.owner_matches,
       checked.version_matches,
       checked.lease_valid,
       TRUE AS membership_matches,
       TRUE AS members_non_terminal,
       TRUE AS request_matches,
       updated.id IS NOT NULL AS applied,
       (SELECT at FROM clock) AS transitioned_at
FROM checked
LEFT JOIN updated ON updated.id = checked.id
ORDER BY checked.id;
