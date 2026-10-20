-- 논리 그룹 후보 delivery id 조건(WITH 목록 조각)이다. transition_group_rows.sql(그룹 잠금)과
-- handoff_group_parents.sql(handoff 정산의 부모 사전 조회)이 같은 조각을 넣으므로 두 문장의 그룹 범위가 같다.
-- $1 직접 delivery id, $2·$3·$4 같은 위치끼리 한 요청 조합인 room_id·kind·identity 후보.
-- 요청 관계를 한 번만 만들고 identity별 등가 join으로 후보를 찾는다.
-- correlated OR EXISTS는 전체 delivery마다 요청 배열을 다시 순회한다.
requested AS MATERIALIZED (
    SELECT DISTINCT room_id, kind, candidate
    FROM unnest($2::text[], $3::text[], $4::text[]) AS requested(room_id, kind, candidate)
), candidate_ids AS (
    SELECT id
    FROM unnest($1::bigint[]) AS direct(id)
    UNION
    SELECT delivery.id
    FROM requested
    JOIN youtube_notification_outbox AS outbox
      ON outbox.kind = requested.kind
     AND btrim(outbox.content_id) = requested.candidate
    JOIN youtube_notification_delivery AS delivery
      ON delivery.outbox_id = outbox.id
     AND delivery.room_id = requested.room_id
    UNION
    SELECT delivery.id
    FROM requested
    JOIN youtube_notification_outbox AS outbox
      ON outbox.kind = requested.kind
     AND COALESCE(outbox.payload->>'canonical_post_id', '') = requested.candidate
    JOIN youtube_notification_delivery AS delivery
      ON delivery.outbox_id = outbox.id
     AND delivery.room_id = requested.room_id
)
