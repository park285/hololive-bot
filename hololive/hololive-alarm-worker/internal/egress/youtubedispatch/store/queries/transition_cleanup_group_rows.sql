-- cleanup 전용 same-logical 조회다. $1은 요청 자식 delivery id다. $2·$3·$4·$5는 같은 위치끼리 한 요청 identity 조합
-- (room_id, kind, candidate)과 그 조합을 만든 논리 키 번호(1부터)다. $6은 논리 키마다 남길 행 수(LogicalGroupLimit+1)다.
-- 한도는 논리 키마다 따로 둔다. batch 전체가 한도 하나를 나눠 쓰면 오래된 과대 그룹이 창을 채워 무관한 키의 관측까지
-- 끊는다. 키 번호마다 그 키의 조합이 찾은 행을 (created_at, id) 순서로 $6행까지만 남기고, $6번째 행에 그 키 번호를
-- saturated_keys로 붙인다. 조합이 다른 키로 풀리는 행을 찾아도 그 행은 조합을 만든 키의 한도를 쓰므로, 한도에 닿으면
-- 그 키만 불완전 관측이 된다. 반환 행은 최대 len($1) + 키 수×$6이다.
-- 그룹 잠금 문장의 후보 관계는 요청을 DISTINCT로 합쳐 delivery id만 남기므로 키 번호를 보존할 수 없어 여기서 따로 만든다.
-- identity 표현식 index가 없어 조합별 LATERAL은 조합마다 outbox를 다시 훑으므로, 등가 hash join을 한 번 하고 키 안에서
-- 순위를 매긴다.
-- 후보 부모·자식을 쥔 cleanup이 경쟁 트랜잭션이 잠근 행을 기다리면 stale sweep과 교착하므로 SKIP LOCKED로 잠근다.
-- 잠근 행은 잠금 뒤 최신 값을 locked=TRUE로, 잠그지 못한 행은 문장 snapshot 값을 locked=FALSE로 돌린다.
WITH requested AS MATERIALIZED (
    SELECT key_no, room_id, kind, candidate
    FROM unnest($2::text[], $3::text[], $4::text[], $5::bigint[]) AS requested(room_id, kind, candidate, key_no)
), matched AS (
    SELECT requested.key_no, delivery.id, delivery.created_at
    FROM requested
    JOIN youtube_notification_outbox AS outbox
      ON outbox.kind = requested.kind
     AND btrim(outbox.content_id) = requested.candidate
    JOIN youtube_notification_delivery AS delivery
      ON delivery.outbox_id = outbox.id
     AND delivery.room_id = requested.room_id
    UNION
    SELECT requested.key_no, delivery.id, delivery.created_at
    FROM requested
    JOIN youtube_notification_outbox AS outbox
      ON outbox.kind = requested.kind
     AND COALESCE(outbox.payload->>'canonical_post_id', '') = requested.candidate
    JOIN youtube_notification_delivery AS delivery
      ON delivery.outbox_id = outbox.id
     AND delivery.room_id = requested.room_id
), ranked AS (
    SELECT key_no,
           id,
           row_number() OVER (PARTITION BY key_no ORDER BY created_at, id) AS key_rank
    FROM matched
), scanned AS MATERIALIZED (
    SELECT observed.id,
           array_agg(observed.key_no) FILTER (WHERE observed.key_rank = $6) AS saturated_keys
    FROM (
        SELECT direct.id, NULL::bigint AS key_no, NULL::bigint AS key_rank
        FROM unnest($1::bigint[]) AS direct(id)
        UNION ALL
        SELECT ranked.id, ranked.key_no, ranked.key_rank
        FROM ranked
        WHERE ranked.key_rank <= $6
    ) AS observed
    GROUP BY observed.id
), locked_rows AS MATERIALIZED (
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
           outbox.sent_at AS outbox_sent_at,
           scanned.saturated_keys
    FROM youtube_notification_delivery AS delivery
    JOIN youtube_notification_outbox AS outbox ON outbox.id = delivery.outbox_id
    JOIN scanned ON scanned.id = delivery.id
    FOR UPDATE OF delivery SKIP LOCKED
)
SELECT locked_rows.id,
       locked_rows.outbox_id,
       locked_rows.room_id,
       locked_rows.status,
       locked_rows.attempt_count,
       locked_rows.next_attempt_at,
       locked_rows.created_at,
       locked_rows.locked_at,
       locked_rows.locked_by,
       locked_rows.sent_at,
       locked_rows.error,
       locked_rows.row_version,
       locked_rows.kind,
       locked_rows.channel_id,
       locked_rows.content_id,
       locked_rows.payload,
       locked_rows.outbox_created_at,
       locked_rows.outbox_sent_at,
       locked_rows.saturated_keys,
       TRUE AS locked
FROM locked_rows
UNION ALL
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
       outbox.sent_at AS outbox_sent_at,
       scanned.saturated_keys,
       FALSE AS locked
FROM scanned
JOIN youtube_notification_delivery AS delivery ON delivery.id = scanned.id
JOIN youtube_notification_outbox AS outbox ON outbox.id = delivery.outbox_id
WHERE NOT EXISTS (SELECT 1 FROM locked_rows WHERE locked_rows.id = scanned.id)
ORDER BY created_at, id;
